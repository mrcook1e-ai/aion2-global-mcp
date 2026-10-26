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
	return m
}

// EnsureMarketQuotes loads cached quotes for serverID and refreshes stale/missing items concurrently from Questlog API.
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
		q, ok := cached[id]
		if forceRefresh || !ok || now.Sub(q.UpdatedAt) > marketCacheTTL {
			toFetch = append(toFetch, id)
		}
	}
	if len(toFetch) == 0 {
		return cached, nil
	}

	var (
		mu       sync.Mutex
		toUpsert []domain.MarketQuote
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for _, id := range toFetch {
		id := id
		g.Go(func() error {
			quotes, err := r.remote.FetchItemMarket(gctx, id)
			if err != nil {
				// Keep stale cached value on transient network error
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			matchedServer := false
			for _, q := range quotes {
				toUpsert = append(toUpsert, q)
				if q.ServerID == serverID {
					qCopy := q
					cached[id] = &qCopy
					matchedServer = true
				}
			}
			// Negative cache entry (0 price, fresh timestamp) so non-tradable items aren't refetched every call!
			if !matchedServer {
				negQuote := domain.MarketQuote{
					ItemID:    id,
					ServerID:  serverID,
					Market:    "none",
					UpdatedAt: now,
				}
				toUpsert = append(toUpsert, negQuote)
				if _, hadOld := cached[id]; !hadOld {
					cached[id] = &negQuote
				}
			}
			return nil
		})
	}
	_ = g.Wait()

	if len(toUpsert) > 0 {
		_ = r.market.UpsertMarketQuotes(ctx, toUpsert)
	}
	return cached, nil
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
