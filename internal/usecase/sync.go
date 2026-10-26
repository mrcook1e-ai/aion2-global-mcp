package usecase

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type SyncSummary struct {
	ItemsUpserted    int    `json:"itemsUpserted"`
	RecipesUpserted  int    `json:"recipesUpserted"`
	EntitiesUpserted int    `json:"entitiesUpserted"`
	LogFilesScanned  int    `json:"logFilesScanned"`
	InvEventsApplied int    `json:"invEventsApplied"`
	DurationMs       int64  `json:"durationMs"`
	Note             string `json:"note,omitempty"`
}

type SyncService struct {
	catalog  domain.CatalogRepository
	invRepo  domain.InventoryRepository
	remote   domain.RemoteCatalogProvider
	localFS  domain.LocalTelemetryReader
	resolver *Resolver
	logger   *slog.Logger

	mu sync.Mutex
}

func NewSyncService(
	catalog domain.CatalogRepository,
	invRepo domain.InventoryRepository,
	remote domain.RemoteCatalogProvider,
	localFS domain.LocalTelemetryReader,
	resolver *Resolver,
	logger *slog.Logger,
) *SyncService {
	return &SyncService{
		catalog:  catalog,
		invRepo:  invRepo,
		remote:   remote,
		localFS:  localFS,
		resolver: resolver,
		logger:   logger,
	}
}

// SyncTelemetry performs a fast incremental sync of local .ndjson inventory logs and local entity dictionaries.
func (s *SyncService) SyncTelemetry(ctx context.Context) (SyncSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()
	var sum SyncSummary
	if s.localFS == nil || !s.localFS.Available() {
		sum.Note = "локальная папка Questlog не найдена на этом хосте"
		return sum, nil
	}

	if entries, err := s.localFS.ReadLocalEntities(ctx, s.resolver.Locale()); err == nil && len(entries) > 0 {
		if err := s.catalog.UpsertEntities(ctx, entries); err == nil {
			sum.EntitiesUpserted += len(entries)
		}
	}

	chars, _ := s.localFS.ReadCharacters(ctx)
	files, evs, err := s.localFS.SyncNDJSONInventory(ctx, s.invRepo, chars)
	sum.LogFilesScanned = files
	sum.InvEventsApplied = evs
	sum.DurationMs = time.Since(start).Milliseconds()
	return sum, err
}

// SyncFullCatalog downloads/refreshes items, crafting recipes (with successRates), stats, bosses, and dungeons from Questlog.gg.
func (s *SyncService) SyncFullCatalog(ctx context.Context) (SyncSummary, error) {
	start := time.Now()
	sum, _ := s.SyncTelemetry(ctx)

	if s.remote == nil {
		sum.DurationMs = time.Since(start).Milliseconds()
		return sum, nil
	}

	locale := s.resolver.Locale()

	if eqItems, err := s.remote.FetchAllEquipmentItems(ctx, locale); err == nil && len(eqItems) > 0 {
		if err := s.catalog.UpsertItems(ctx, eqItems); err == nil {
			sum.ItemsUpserted += len(eqItems)
		}
	}
	if ahItems, err := s.remote.FetchAllAuctionItems(ctx, locale); err == nil && len(ahItems) > 0 {
		if err := s.catalog.UpsertItems(ctx, ahItems); err == nil {
			sum.ItemsUpserted += len(ahItems)
		}
	}
	if recipes, err := s.remote.FetchAllRecipes(ctx, locale); err == nil && len(recipes) > 0 {
		if err := s.catalog.UpsertRecipes(ctx, recipes); err == nil {
			sum.RecipesUpserted += len(recipes)
		}
		// Ensure all product/combo/ingredient items from recipes are also registered in items table
		var recItems []domain.Item
		for _, r := range recipes {
			if r.ProductItemID > 0 && r.ProductItemName != "" {
				recItems = append(recItems, domain.Item{ID: r.ProductItemID, Locale: locale, Name: r.ProductItemName, Grade: r.Grade, SubCategory: r.SubCategory})
			}
			if r.ComboItemID > 0 && r.ComboItemName != "" {
				recItems = append(recItems, domain.Item{ID: r.ComboItemID, Locale: locale, Name: r.ComboItemName, Grade: r.Grade, SubCategory: r.SubCategory})
			}
			for _, ing := range r.Ingredients {
				if ing.ItemID > 0 && ing.ItemName != "" {
					recItems = append(recItems, domain.Item{ID: ing.ItemID, Locale: locale, Name: ing.ItemName, Grade: ing.Grade})
				}
			}
		}
		_ = s.catalog.UpsertItems(ctx, recItems)
	}

	for _, kind := range []domain.EntityKind{
		domain.EntityStat,
		domain.EntityBoss,
		domain.EntityDungeon,
		domain.EntitySkill,
		domain.EntityWing,
		domain.EntityPet,
	} {
		if ents, err := s.remote.FetchEntities(ctx, kind, locale); err == nil && len(ents) > 0 {
			if err := s.catalog.UpsertEntities(ctx, ents); err == nil {
				sum.EntitiesUpserted += len(ents)
			}
		}
	}

	sum.DurationMs = time.Since(start).Milliseconds()
	return sum, nil
}

// RunBackgroundWatcher periodically tails Questlog's .ndjson logs so character inventory stays fresh in SQLite.
func (s *SyncService) RunBackgroundWatcher(ctx context.Context, interval time.Duration) {
	if s.localFS == nil || !s.localFS.Available() {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if sum, err := s.SyncTelemetry(ctx); err != nil && s.logger != nil {
		s.logger.Warn("initial telemetry sync warning", "err", err)
	} else if s.logger != nil && sum.InvEventsApplied > 0 {
		s.logger.Info("indexed questlog inventory events", "files", sum.LogFilesScanned, "events", sum.InvEventsApplied)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sum, err := s.SyncTelemetry(ctx)
			if err != nil && s.logger != nil {
				s.logger.Warn("background telemetry sync error", "err", err)
			} else if s.logger != nil && sum.InvEventsApplied > 0 {
				s.logger.Info("live inventory update", "events", sum.InvEventsApplied)
			}
		}
	}
}
