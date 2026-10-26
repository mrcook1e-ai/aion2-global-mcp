package usecase

import (
	"context"
	"fmt"
	"sort"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type ProfitableCraftsQuery struct {
	Character       string  // Optional: character name or "serverId:Name"; defaults to active character
	ServerID        int     // Optional override when Character is not available
	Profession      string  // Required: e.g. "jewelcrafting" / "ювелирка"
	MasteryLevel    *int    // Optional override; if nil, reads saved level from character_professions
	MinMasteryLevel int     // Optional lower bound to skip trivial low-tier crafts
	SubCategory     string  // Optional filter (e.g. "ring", "necklace", "bow")
	UseInventory    bool    // Include character bag + storage stacks from .ndjson
	SortBy          string  // "net_profit" (default, true market profit) or "cash_flow"
	TaxRate         float64 // Default 0.12 (12%)
	ForceRefresh    bool    // Force live market refresh ignoring 25m TTL
	TopN            int     // Default 15
}

type ProfitableCraftsReport struct {
	CharacterKey      string                   `json:"characterKey,omitempty"`
	ServerID          domain.ServerID          `json:"serverId"`
	Profession        domain.Profession        `json:"profession"`
	PlayerLevel       int                      `json:"playerLevel"`
	LevelSource       string                   `json:"levelSource"` // "saved_profile", "query_override", or "unconfigured"
	RecipesEvaluated  int                      `json:"recipesEvaluated"`
	InventoryItems    int                      `json:"inventoryItemsMatched"`
	TaxRate           float64                  `json:"taxRate"`
	TopCrafts         []domain.CraftEvaluation `json:"topCrafts"`
	ActionableSummary string                   `json:"actionableSummary,omitempty"`
}

type CraftService struct {
	catalog  domain.CatalogRepository
	invRepo  domain.InventoryRepository
	profRepo domain.ProfessionRepository
	charSvc  *CharacterService
	syncSvc  *SyncService
	resolver *Resolver
}

func NewCraftService(
	catalog domain.CatalogRepository,
	invRepo domain.InventoryRepository,
	profRepo domain.ProfessionRepository,
	charSvc *CharacterService,
	syncSvc *SyncService,
	resolver *Resolver,
) *CraftService {
	return &CraftService{
		catalog:  catalog,
		invRepo:  invRepo,
		profRepo: profRepo,
		charSvc:  charSvc,
		syncSvc:  syncSvc,
		resolver: resolver,
	}
}

func (s *CraftService) ScanProfitableCrafts(ctx context.Context, q ProfitableCraftsQuery) (*ProfitableCraftsReport, error) {
	prof := domain.NormalizeProfession(q.Profession)
	if prof == "" {
		return nil, fmt.Errorf("укажи профессию: jewelcrafting (ювелирка), blacksmithing (кузнечка), tailoring (портняжка), alchemy (алхимия) или cooking (кулинария)")
	}

	_, _ = s.syncSvc.SyncTelemetry(ctx)

	var (
		charKey   string
		serverID  = domain.ServerID(q.ServerID)
		raceQual  string
		playerLvl int
		lvlSource = "query_override"
	)

	if ch, err := s.charSvc.FindCharacter(ctx, q.Character); err == nil && ch != nil {
		charKey = ch.Key
		if serverID == 0 {
			serverID = ch.ServerID
		}
		raceQual = domain.NormalizeRaceQualification(ch.Race)
	}
	if serverID <= 0 {
		return nil, fmt.Errorf("не удалось определить serverId персонажа — передай serverId явно или проверь подключение к Questlog")
	}

	if q.MasteryLevel != nil {
		playerLvl = *q.MasteryLevel
	} else if charKey != "" {
		saved, err := s.profRepo.GetProfession(ctx, charKey, prof)
		if err != nil {
			return nil, err
		}
		if saved == nil {
			return nil, fmt.Errorf(
				"для персонажа %s ещё не сохранён уровень ремесла %q в БД. Спроси у пользователя его текущий уровень %s (или передай masteryLevel) и сохрани через set_profession",
				charKey, prof, prof,
			)
		}
		playerLvl = saved.MasteryLevel
		lvlSource = "saved_profile"
	} else {
		return nil, fmt.Errorf("укажи masteryLevel для профессии %s", prof)
	}

	recipes, err := s.catalog.SearchRecipes(ctx, domain.RecipeFilter{
		Profession:        prof,
		SubCategory:       q.SubCategory,
		QualificationRace: raceQual,
		MaxMasteryLevel:   &playerLvl,
		MinMasteryLevel:   q.MinMasteryLevel,
		ExcludeGuildCraft: true,
	})
	if err != nil {
		return nil, err
	}
	if len(recipes) == 0 {
		return &ProfitableCraftsReport{
			CharacterKey:      charKey,
			ServerID:          serverID,
			Profession:        prof,
			PlayerLevel:       playerLvl,
			LevelSource:       lvlSource,
			ActionableSummary: "В локальном каталоге нет рецептов под этот фильтр — запусти sync_catalog, если база ещё не инициализирована.",
		}, nil
	}

	owned := make(map[domain.ItemID]int64)
	if q.UseInventory && charKey != "" {
		if agg, err := s.invRepo.GetAggregatedInventory(ctx, charKey); err == nil {
			for id, a := range agg {
				owned[id] = a.TotalCount
			}
		}
	}

	// Gather all unique product, combo, and ingredient IDs to batch-fetch live market prices concurrently!
	var marketItemIDs []domain.ItemID
	matchedInv := make(map[domain.ItemID]struct{})
	for _, r := range recipes {
		if r.ProductItemID > 0 {
			marketItemIDs = append(marketItemIDs, r.ProductItemID)
		}
		if r.ComboItemID > 0 {
			marketItemIDs = append(marketItemIDs, r.ComboItemID)
		}
		for _, ing := range r.Ingredients {
			marketItemIDs = append(marketItemIDs, ing.ItemID)
			if owned[ing.ItemID] > 0 {
				matchedInv[ing.ItemID] = struct{}{}
			}
		}
	}

	quotes, err := s.resolver.EnsureMarketQuotes(ctx, serverID, marketItemIDs, q.ForceRefresh)
	if err != nil {
		return nil, err
	}

	tax := q.TaxRate
	if tax <= 0 {
		tax = domain.DefaultMarketTaxRate
	}

	evals := make([]domain.CraftEvaluation, 0, len(recipes))
	for _, r := range recipes {
		ev := domain.EvaluateRecipe(r, playerLvl, owned, quotes, tax)
		// Filter out untradable dead recipes with zero expected revenue
		if ev.ExpectedNetRevenue <= 0 {
			continue
		}
		evals = append(evals, ev)
	}

	sortByCashFlow := q.SortBy == "cash_flow"
	sort.Slice(evals, func(i, j int) bool {
		// Always rank complete market-priced evaluations above incomplete ones
		if evals[i].IncompletePricing != evals[j].IncompletePricing {
			return !evals[i].IncompletePricing
		}
		if sortByCashFlow {
			return evals[i].CashFlowProfit > evals[j].CashFlowProfit
		}
		return evals[i].NetProfitMarket > evals[j].NetProfitMarket
	})

	topN := q.TopN
	if topN <= 0 || topN > 50 {
		topN = 15
	}
	if len(evals) > topN {
		evals = evals[:topN]
	}

	return &ProfitableCraftsReport{
		CharacterKey:     charKey,
		ServerID:         serverID,
		Profession:       prof,
		PlayerLevel:      playerLvl,
		LevelSource:      lvlSource,
		RecipesEvaluated: len(recipes),
		InventoryItems:   len(matchedInv),
		TaxRate:          tax,
		TopCrafts:        evals,
	}, nil
}

// EvaluateSingleRecipe calculates full economics for one specific recipe ID or fuzzy recipe/item query.
func (s *CraftService) EvaluateSingleRecipe(
	ctx context.Context,
	recipeID domain.RecipeID,
	recipeQuery string,
	character string,
	serverID int,
	masteryLevel *int,
	forceRefresh bool,
) (*domain.CraftEvaluation, error) {
	_, _ = s.syncSvc.SyncTelemetry(ctx)

	var rec *domain.Recipe
	var err error
	if recipeID > 0 {
		rec, err = s.catalog.GetRecipe(ctx, recipeID)
	} else {
		matches, sErr := s.catalog.SearchRecipes(ctx, domain.RecipeFilter{Query: recipeQuery, Limit: 1})
		err = sErr
		if len(matches) > 0 {
			rec = &matches[0]
		}
	}
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("рецепт не найден")
	}

	var (
		charKey string
		srvID   = domain.ServerID(serverID)
		lvl     = rec.MasteryLevel
	)
	if ch, err := s.charSvc.FindCharacter(ctx, character); err == nil && ch != nil {
		charKey = ch.Key
		if srvID == 0 {
			srvID = ch.ServerID
		}
		if saved, _ := s.profRepo.GetProfession(ctx, charKey, rec.Profession); saved != nil && saved.MasteryLevel > lvl {
			lvl = saved.MasteryLevel
		}
	}
	if masteryLevel != nil {
		lvl = *masteryLevel
	}
	if srvID <= 0 {
		return nil, fmt.Errorf("укажи serverId для расчёта рыночных цен")
	}

	owned := make(map[domain.ItemID]int64)
	if charKey != "" {
		if agg, err := s.invRepo.GetAggregatedInventory(ctx, charKey); err == nil {
			for id, a := range agg {
				owned[id] = a.TotalCount
			}
		}
	}

	ids := []domain.ItemID{rec.ProductItemID, rec.ComboItemID}
	for _, ing := range rec.Ingredients {
		ids = append(ids, ing.ItemID)
	}
	quotes, err := s.resolver.EnsureMarketQuotes(ctx, srvID, ids, forceRefresh)
	if err != nil {
		return nil, err
	}

	ev := domain.EvaluateRecipe(*rec, lvl, owned, quotes, domain.DefaultMarketTaxRate)
	return &ev, nil
}
