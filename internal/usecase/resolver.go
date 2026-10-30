package usecase

import (
	"context"
	"encoding/json"
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
	missedEnt  map[string]time.Time
	bulkLoaded map[domain.EntityKind]bool
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
		missedEnt:  make(map[string]time.Time),
		bulkLoaded: make(map[domain.EntityKind]bool),
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
			if err != nil {
				r.negMu.Lock()
				r.missedItem[id] = time.Now().Add(15 * time.Second)
				r.negMu.Unlock()
				return nil
			}
			if it == nil {
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

// ResolveEntities resolves localized entities (skills, wings, pets, titles, etc.) with read-through caching.
// When needSkillSpecs is true for EntitySkill, skills lacking cached specialization tripods are enriched via database.getSkill.
func (r *Resolver) ResolveEntities(ctx context.Context, kind domain.EntityKind, ids []int64, needSkillSpecs bool) map[int64]domain.EntityEntry {
	expanded := make([]int64, 0, len(ids)*2)
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		expanded = append(expanded, id)
		if kind == domain.EntitySkill {
			if base := domain.BaseSkillID(id); base != id {
				expanded = append(expanded, base)
			}
		}
	}
	unique := deduplicateInt64s(expanded)
	if len(unique) == 0 {
		return map[int64]domain.EntityEntry{}
	}

	found, err := r.catalog.GetEntitiesByIDs(ctx, kind, unique, r.locale)
	if err != nil {
		found = make(map[int64]domain.EntityEntry)
	}
	if r.remote == nil {
		return found
	}

	// Bulk warm-up for entity catalogs when missing from SQLite
	if (kind == domain.EntityWing || kind == domain.EntityPet || kind == domain.EntityBoss || kind == domain.EntityDungeon || kind == domain.EntityStat || (kind == domain.EntitySkill && !needSkillSpecs)) && len(found) < len(unique) {
		r.negMu.Lock()
		alreadyBulk := r.bulkLoaded[kind]
		if !alreadyBulk {
			r.bulkLoaded[kind] = true
		}
		r.negMu.Unlock()

		if !alreadyBulk {
			if bulk, bErr := r.remote.FetchEntities(ctx, kind, r.locale); bErr == nil && len(bulk) > 0 {
				_ = r.catalog.UpsertEntities(ctx, bulk)
				for _, e := range bulk {
					found[e.ID] = e
				}
			}
		}
	}

	var toFetch []int64
	now := time.Now()
	r.negMu.Lock()
	for _, id := range unique {
		ent, ok := found[id]
		needsFetch := !ok
		if ok && kind == domain.EntitySkill && needSkillSpecs && !hasCachedSkillSpecs(ent.MetaJSON) {
			needsFetch = true
		}
		if !needsFetch {
			continue
		}
		negKey := fmt.Sprintf("%s:%d", kind, id)
		if retryAfter, neg := r.missedEnt[negKey]; neg && now.Before(retryAfter) {
			continue
		}
		toFetch = append(toFetch, id)
	}
	r.negMu.Unlock()

	if len(toFetch) == 0 {
		return found
	}

	var (
		mu      sync.Mutex
		fetched []domain.EntityEntry
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for _, id := range toFetch {
		id := id
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			ent, fErr := r.remote.FetchEntityByID(gctx, kind, id, r.locale)
			if fErr != nil {
				r.negMu.Lock()
				r.missedEnt[fmt.Sprintf("%s:%d", kind, id)] = time.Now().Add(15 * time.Second)
				r.negMu.Unlock()
				return nil
			}
			if ent == nil {
				r.negMu.Lock()
				r.missedEnt[fmt.Sprintf("%s:%d", kind, id)] = time.Now().Add(30 * time.Minute)
				r.negMu.Unlock()
				return nil
			}
			mu.Lock()
			fetched = append(fetched, *ent)
			found[id] = *ent
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	if len(fetched) > 0 {
		_ = r.catalog.UpsertEntities(ctx, fetched)
	}

	return found
}

func hasCachedSkillSpecs(metaJSON string) bool {
	if metaJSON == "" {
		return false
	}
	var env domain.SkillMetaEnvelope
	return json.Unmarshal([]byte(metaJSON), &env) == nil && env.HasSpecs
}

// knownAutoAttackNames maps internal auto-attack and item-proc skill IDs to human labels.
// These IDs are engine-internal and never returned by database.getSkill.
var knownAutoAttackNames = map[int64]string{
	// Class auto-attacks (base ID per class)
	10000000: "Автоатака (Гладиатор)",
	10100000: "Автоатака (Страж)",
	11000000: "Автоатака (Убийца)",
	11100000: "Автоатака (Стрелок)",
	12000000: "Автоатака (Волшебник)",
	12100000: "Автоатака (Целитель)",
	13000000: "Автоатака (Чародей)",
	13100000: "Автоатака (Бард)",
	14000000: "Автоатака (Рейнджер)",
	14100000: "Автоатака (Снайпер)",
	15000000: "Автоатака (Инженер)",
	15100000: "Автоатака (Райдер)",
	16000000: "Автоатака (Элементалист)",
	17000000: "Автоатака (Мистик)",
	18000000: "Автоатака (Воин духа)",
	// Common item/rune procs and engine effects
	5004: "Прок оружия",
	5005: "Прок руны",
	5006: "Прок щита",
	5007: "Прок брони",
	// God stone procs
	100055: "Прок божественного камня",
	100056: "Прок божественного камня",
	100057: "Прок божественного камня",
}

// SkillLabel resolves a skill name (falling back to parent BaseSkillID for multi-stage rotation IDs,
// with reactive on-demand Questlog query and SQLite persistence for missing IDs).
func (r *Resolver) SkillLabel(skills map[int64]domain.EntityEntry, skillID int64) string {
	if ent, ok := skills[skillID]; ok && ent.Name != "" {
		return ent.Name
	}
	// Check auto-attack / proc hardcoded names before expensive API calls
	if name, ok := knownAutoAttackNames[skillID]; ok {
		return name
	}
	base := domain.BaseSkillID(skillID)
	if base != skillID {
		if ent, ok := skills[base]; ok && ent.Name != "" {
			return ent.Name
		}
		if name, ok := knownAutoAttackNames[base]; ok {
			return name
		}
	}
	// Reactive on-demand Read-Through: query Questlog database.getSkill and persist to SQLite
	if r.remote != nil && base > 0 {
		bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		res := r.ResolveEntities(bgCtx, domain.EntitySkill, []int64{base}, false)
		cancel()
		if ent, ok := res[base]; ok && ent.Name != "" {
			return ent.Name
		}
	}
	return fmt.Sprintf("skill#%d", skillID)
}

// SkillSpecDescriptions maps selected specialization slot numbers (e.g. [1, 2]) to their localized descriptions.
func SkillSpecDescriptions(ent domain.EntityEntry, slots []int) []string {
	if len(slots) == 0 || ent.MetaJSON == "" {
		return nil
	}
	var env domain.SkillMetaEnvelope
	if err := json.Unmarshal([]byte(ent.MetaJSON), &env); err != nil || len(env.Specializations) == 0 {
		return nil
	}
	bySlot := make(map[int]string, len(env.Specializations))
	for _, sp := range env.Specializations {
		bySlot[sp.Slot] = sp.Specialized
	}
	var out []string
	for _, sl := range slots {
		if txt := bySlot[sl]; txt != "" {
			out = append(out, fmt.Sprintf("[%d] %s", sl, txt))
		}
	}
	return out
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
		m = make(map[int64]domain.EntityEntry)
	}
	if len(m) == 0 && r.locale != "en" {
		m, _ = r.catalog.GetEntitiesByKind(ctx, kind, "en")
	}
	// Reactive auto-fill: if catalog table in SQLite is empty, pull full list from Questlog and persist!
	if len(m) == 0 && r.remote != nil {
		if bulk, bErr := r.remote.FetchEntities(ctx, kind, r.locale); bErr == nil && len(bulk) > 0 {
			_ = r.catalog.UpsertEntities(ctx, bulk)
			m = make(map[int64]domain.EntityEntry, len(bulk))
			for _, e := range bulk {
				m[e.ID] = e
			}
		}
	}
	return m
}

// NpcOrBossLabel resolves an NPC/Boss name across EntityBoss and EntityNPC dictionaries,
// with reactive on-demand Questlog query and SQLite persistence for missing IDs.
func (r *Resolver) NpcOrBossLabel(bosses, npcs map[int64]domain.EntityEntry, id domain.NpcID) string {
	if b, ok := bosses[int64(id)]; ok && b.Name != "" {
		return b.Name
	}
	if n, ok := npcs[int64(id)]; ok && n.Name != "" {
		return n.Name
	}
	// On-demand reactive Read-Through: query Questlog database.getNpc and persist to SQLite!
	if r.remote != nil && id > 0 {
		bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		res := r.ResolveEntities(bgCtx, domain.EntityNPC, []int64{int64(id)}, false)
		cancel()
		if ent, ok := res[int64(id)]; ok && ent.Name != "" {
			return ent.Name
		}
	}
	return fmt.Sprintf("NPC #%d", id)
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
				mu.Lock()
				negQuote := domain.MarketQuote{
					ItemID:    id,
					ServerID:  serverID,
					Market:    "none",
					UpdatedAt: time.Now().UTC(),
				}
				pending = append(pending, negQuote)
				if _, hadOld := cached[id]; !hadOld {
					cached[id] = &negQuote
				}
				mu.Unlock()
				return nil
			}

			fetchNow := time.Now().UTC()
			shouldFlush := false
			mu.Lock()
			matchedServer := false
			for _, q := range quotes {
				q.HydrateFreshness(fetchNow)
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
					UpdatedAt: fetchNow,
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

func deduplicateInt64s(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
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
