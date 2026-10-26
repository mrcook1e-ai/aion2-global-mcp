package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

const marketCacheTTL = 25 * time.Minute

// Resolver resolves raw numeric IDs into localized domain objects via SQLite with read-through fallback to Questlog API.
type Resolver struct {
	catalog domain.CatalogRepository
	market  domain.MarketRepository
	remote  domain.RemoteCatalogProvider
	locale  string

	negMu      sync.Mutex
	missedItem map[domain.ItemID]time.Time
}

func NewResolver(
	catalog domain.CatalogRepository,
	market domain.MarketRepository,
	remote domain.RemoteCatalogProvider,
	locale string,
) *Resolver {
	if locale == "" {
		locale = "ru"
	}
	return &Resolver{
		catalog:    catalog,
		market:     market,
		remote:     remote,
		locale:     locale,
		missedItem: make(map[domain.ItemID]time.Time),
	}
}

func (r *Resolver) Locale() string { return r.locale }

// ResolveItems resolves a batch of ItemIDs from SQLite and fetches any unknown IDs concurrently via read-through API.
func (r *Resolver) ResolveItems(ctx context.Context, ids []domain.ItemID) (map[domain.ItemID]domain.Item, error) {
	unique := deduplicateItemIDs(ids)
	if len(unique) == 0 {
		return map[domain.ItemID]domain.Item{}, nil
	}

	found, err := r.catalog.GetItemsByIDs(ctx, unique)
	if err != nil {
		return nil, err
	}
	if r.remote == nil {
		return found, nil
	}

	var missing []domain.ItemID
	now := time.Now()
	r.negMu.Lock()
	for _, id := range unique {
		if _, ok := found[id]; ok {
			continue
		}
		if retryAfter, neg := r.missedItem[id]; neg && now.Before(retryAfter) {
			continue
		}
		missing = append(missing, id)
	}
	r.negMu.Unlock()

	if len(missing) == 0 {
		return found, nil
	}

	var (
		mu      sync.Mutex
		fetched []domain.Item
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for _, id := range missing {
		id := id
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			it, err := r.remote.FetchItem(gctx, id, r.locale)
			if err != nil || it == nil {
				r.negMu.Lock()
				r.missedItem[id] = time.Now().Add(30 * time.Minute)
				r.negMu.Unlock()
				return nil
			}
			mu.Lock()
			fetched = append(fetched, *it)
			found[id] = *it
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	if len(fetched) > 0 {
		_ = r.catalog.UpsertItems(ctx, fetched)
	}
	return found, nil
}

// EnsureNPCPrices enriches non-tradable / unpriced ingredient items via database.getItem to capture vendor NPC buy prices.
func (r *Resolver) EnsureNPCPrices(ctx context.Context, ids []domain.ItemID) (map[domain.ItemID]int64, error) {
	unique := deduplicateItemIDs(ids)
	out := make(map[domain.ItemID]int64, len(unique))
	if len(unique) == 0 {
		return out, nil
	}

	itemsMap, err := r.catalog.GetItemsByIDs(ctx, unique)
	if err != nil {
		return out, err
	}

	var toInspect []domain.ItemID
	for _, id := range unique {
		it, ok := itemsMap[id]
		if ok && it.NPCBuyPrice > 0 {
			out[id] = it.NPCBuyPrice
			continue
		}
		// Only inspect items not confirmed as auction-tradable that haven't been deep-fetched yet
		if r.remote != nil && (!ok || (it.Tradable == nil && it.UpdatedAt.IsZero()) || (it.Tradable != nil && !*it.Tradable && it.NPCBuyPrice == 0 && it.UpdatedAt.IsZero())) {
			toInspect = append(toInspect, id)
		} else if r.remote != nil && ok && (it.Tradable == nil || !*it.Tradable) && it.NPCBuyPrice == 0 {
			r.negMu.Lock()
			retryAfter, seen := r.missedItem[id]
			r.negMu.Unlock()
			if !seen || time.Now().After(retryAfter) {
				toInspect = append(toInspect, id)
			}
		}
	}

	if len(toInspect) == 0 || r.remote == nil {
		return out, nil
	}

	var (
		mu      sync.Mutex
		fetched []domain.Item
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for _, id := range toInspect {
		id := id
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			it, err := r.remote.FetchItem(gctx, id, r.locale)
			r.negMu.Lock()
			r.missedItem[id] = time.Now().Add(6 * time.Hour)
			r.negMu.Unlock()
			if err != nil || it == nil {
				return nil
			}
			mu.Lock()
			fetched = append(fetched, *it)
			if it.NPCBuyPrice > 0 {
				out[id] = it.NPCBuyPrice
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	if len(fetched) > 0 {
		_ = r.catalog.UpsertItems(ctx, fetched)
	}
	return out, nil
}

func (r *Resolver) ItemLabel(items map[domain.ItemID]domain.Item, id domain.ItemID) string {
	if it, ok := items[id]; ok && it.Name != "" {
		return it.Name
	}
	return fmt.Sprintf("#%d", id)
}

func (r *Resolver) EntityMap(ctx context.Context, kind domain.EntityKind) map[int64]domain.EntityEntry {
	m, err := r.catalog.GetEntitiesByKind(ctx, kind, r.locale)
	if err != nil {
		return map[int64]domain.EntityEntry{}
	}
	if len(m) == 0 && r.locale != "en" {
		m, _ = r.catalog.GetEntitiesByKind(ctx, kind, "en")
	}
	return m
}

// EnsureMarketQuotes loads cached quotes for serverID and refreshes stale/missing items concurrently,
// persisting chunks incrementally to SQLite so progress is never lost on cancellation.
func (r *Resolver) EnsureMarketQuotes(
	ctx context.Context,
	serverID domain.ServerID,
	ids []domain.ItemID,
	forceRefresh bool,
) (map[domain.ItemID]*domain.MarketQuote, error) {
	unique := deduplicateItemIDs(ids)
	if len(unique) == 0 || serverID <= 0 {
		return map[domain.ItemID]*domain.MarketQuote{}, nil
	}

	cached, err := r.market.GetMarketQuotes(ctx, serverID, unique)
	if err != nil {
		return nil, err
	}
	if r.remote == nil {
		return cached, nil
	}

	now := time.Now().UTC()
	var toFetch []domain.ItemID
	for _, id := range unique {
		if domain.IsKinahItem(id) {
			continue
		}
		q, ok := cached[id]
		if forceRefresh || !ok || now.Sub(q.UpdatedAt) > marketCacheTTL {
			toFetch = append(toFetch, id)
		}
	}
	if len(toFetch) == 0 {
		return cached, nil
	}

	var (
		mu      sync.Mutex
		pending []domain.MarketQuote
	)
	flushPending := func(flushCtx context.Context) {
		mu.Lock()
		if len(pending) == 0 {
			mu.Unlock()
			return
		}
		batch := make([]domain.MarketQuote, len(pending))
		copy(batch, pending)
		pending = pending[:0]
		mu.Unlock()
		_ = r.market.UpsertMarketQuotes(flushCtx, batch)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for _, id := range toFetch {
		id := id
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			quotes, err := r.remote.FetchItemMarket(gctx, id)
			if err != nil {
				return nil
			}

			shouldFlush := false
			mu.Lock()
			matchedServer := false
			for _, q := range quotes {
				pending = append(pending, q)
				if q.ServerID == serverID {
					qCopy := q
					cached[id] = &qCopy
					matchedServer = true
				}
			}
			if !matchedServer {
				negQuote := domain.MarketQuote{
					ItemID:    id,
					ServerID:  serverID,
					Market:    "none",
					UpdatedAt: now,
				}
				pending = append(pending, negQuote)
				if _, hadOld := cached[id]; !hadOld {
					cached[id] = &negQuote
				}
			}
			if len(pending) >= 40 {
				shouldFlush = true
			}
			mu.Unlock()

			if shouldFlush {
				flushPending(context.WithoutCancel(ctx))
			}
			return nil
		})
	}
	_ = g.Wait()
	flushPending(context.WithoutCancel(ctx))

	return cached, ctx.Err()
}

func deduplicateItemIDs(ids []domain.ItemID) []domain.ItemID {
	seen := make(map[domain.ItemID]struct{}, len(ids))
	out := make([]domain.ItemID, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}
