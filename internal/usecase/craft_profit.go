package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type ProfitableCraftsQuery struct {
	Character       string  // Optional: character name or "serverId:Name"; defaults to active character
	ServerID        int     // Optional override when Character is not available
	Profession      string  // Required: e.g. "jewelcrafting" / "ювелирка"
	MasteryGrade    string  // Optional tier label ("beginner"/"новичок", "intermediate"/"профи") when overriding level
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
	PlayerLevel       int                      `json:"playerLevel"`  // internal continuous scale (1..100+)
	DisplayLevel      string                   `json:"displayLevel"` // e.g. "intermediate 26 (internal 76)"
	LevelSource       string                   `json:"levelSource"`  // "saved_profile" or "query_override"
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
		charKey    string
		serverID   = domain.ServerID(q.ServerID)
		raceQual   string
		playerLvl  int
		displayLvl string
		lvlSource  = "query_override"
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
		_, playerLvl, displayLvl = domain.NormalizeMastery(q.MasteryGrade, *q.MasteryLevel)
	} else if charKey != "" {
		saved, err := s.profRepo.GetProfession(ctx, charKey, prof)
		if err != nil {
			return nil, err
		}
		if saved == nil {
			return nil, fmt.Errorf(
				"для персонажа %s ещё не сохранён уровень ремесла %q в БД. Спроси у пользователя его грейд (Новичок / Профи) и уровень, затем сохрани через set_profession",
				charKey, prof,
			)
		}
		playerLvl = saved.MasteryLevel
		displayLvl = saved.DisplayLevel
		lvlSource = "saved_profile"
	} else {
		return nil, fmt.Errorf("укажи masteryLevel (и при необходимости masteryGrade) для профессии %s", prof)
	}

	rawRecipes, err := s.catalog.SearchRecipes(ctx, domain.RecipeFilter{
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

	// Strictly drop 0-ingredient morph/placeholder shells (e.g. 2430xxxx) so fake +40M profits never appear!
	recipes := make([]domain.Recipe, 0, len(rawRecipes))
	for _, r := range rawRecipes {
		if len(r.Ingredients) > 0 {
			recipes = append(recipes, r)
		}
	}

	if len(recipes) == 0 {
		return &ProfitableCraftsReport{
			CharacterKey:      charKey,
			ServerID:          serverID,
			Profession:        prof,
			PlayerLevel:       playerLvl,
			DisplayLevel:      displayLvl,
			LevelSource:       lvlSource,
			ActionableSummary: "Нет валидных рецептов с ингредиентами под этот фильтр.",
		}, nil
	}

	owned, boundByName := s.loadOwnedAndBoundMaps(ctx, charKey, q.UseInventory)

	// Map bound "(привяз.)" resources to their tradable recipe ingredient IDs by normalized localized base name!
	boundOwned := make(map[domain.ItemID]int64)
	var allItemIDs []domain.ItemID
	var allIngIDs []domain.ItemID
	matchedInv := make(map[domain.ItemID]struct{})

	for _, r := range recipes {
		if r.ProductItemID > 0 {
			allItemIDs = append(allItemIDs, r.ProductItemID)
		}
		if r.ComboItemID > 0 {
			allItemIDs = append(allItemIDs, r.ComboItemID)
		}
		for _, ing := range r.Ingredients {
			allItemIDs = append(allItemIDs, ing.ItemID)
			allIngIDs = append(allIngIDs, ing.ItemID)
			if baseKey := strings.ToLower(strings.TrimSpace(ing.ItemName)); baseKey != "" {
				if bCnt := boundByName[baseKey]; bCnt > 0 {
					boundOwned[ing.ItemID] = bCnt
				}
			}
			if owned[ing.ItemID] > 0 || boundOwned[ing.ItemID] > 0 {
				matchedInv[ing.ItemID] = struct{}{}
			}
		}
	}

	itemsMeta, _ := s.resolver.ResolveItems(ctx, allItemIDs)
	var marketCandidateIDs []domain.ItemID
	for _, id := range deduplicateItemIDs(allItemIDs) {
		it, ok := itemsMeta[id]
		// Skip querying AH for items explicitly marked non-tradable (e.g. bound NPC consumables)
		if ok && it.Tradable != nil && !*it.Tradable {
			continue
		}
		marketCandidateIDs = append(marketCandidateIDs, id)
	}

	quotes, err := s.resolver.EnsureMarketQuotes(ctx, serverID, marketCandidateIDs, q.ForceRefresh)
	if err != nil {
		return nil, err
	}

	// Resolve NPC vendor prices for any ingredients that have no active/median market quote!
	var unpricedIngIDs []domain.ItemID
	for _, id := range deduplicateItemIDs(allIngIDs) {
		price, _ := quotes[id].EffectiveUnitPrice()
		if price <= 0 {
			unpricedIngIDs = append(unpricedIngIDs, id)
		}
	}
	npcPrices, _ := s.resolver.EnsureNPCPrices(ctx, unpricedIngIDs)

	tax := q.TaxRate
	if tax <= 0 {
		tax = domain.DefaultMarketTaxRate
	}

	evals := make([]domain.CraftEvaluation, 0, len(recipes))
	for _, r := range recipes {
		ev := domain.EvaluateRecipe(r, playerLvl, owned, boundOwned, quotes, npcPrices, tax)
		// Exclude untradable or unpriced recipes so top results are 100% actionable!
		if ev.ExpectedNetRevenue <= 0 || ev.IncompletePricing {
			continue
		}
		evals = append(evals, ev)
	}

	sortByCashFlow := q.SortBy == "cash_flow"
	sort.Slice(evals, func(i, j int) bool {
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
		DisplayLevel:     displayLvl,
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
	masteryGrade string,
	masteryLevel *int,
	forceRefresh bool,
) (*domain.CraftEvaluation, error) {
	_, _ = s.syncSvc.SyncTelemetry(ctx)

	var rec *domain.Recipe
	var err error
	if recipeID > 0 {
		rec, err = s.catalog.GetRecipe(ctx, recipeID)
	} else {
		matches, sErr := s.catalog.SearchRecipes(ctx, domain.RecipeFilter{Query: recipeQuery, Limit: 5})
		err = sErr
		for i := range matches {
			if len(matches[i].Ingredients) > 0 {
				rec = &matches[i]
				break
			}
		}
		if rec == nil && len(matches) > 0 {
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
		_, lvl, _ = domain.NormalizeMastery(masteryGrade, *masteryLevel)
	}
	if srvID <= 0 {
		return nil, fmt.Errorf("укажи serverId для расчёта рыночных цен")
	}

	owned, boundByName := s.loadOwnedAndBoundMaps(ctx, charKey, true)
	boundOwned := make(map[domain.ItemID]int64)

	ids := []domain.ItemID{rec.ProductItemID, rec.ComboItemID}
	var ingIDs []domain.ItemID
	for _, ing := range rec.Ingredients {
		ids = append(ids, ing.ItemID)
		ingIDs = append(ingIDs, ing.ItemID)
		if baseKey := strings.ToLower(strings.TrimSpace(ing.ItemName)); baseKey != "" {
			if bCnt := boundByName[baseKey]; bCnt > 0 {
				boundOwned[ing.ItemID] = bCnt
			}
		}
	}

	quotes, err := s.resolver.EnsureMarketQuotes(ctx, srvID, ids, forceRefresh)
	if err != nil {
		return nil, err
	}
	var unpricedIngs []domain.ItemID
	for _, iid := range ingIDs {
		if p, _ := quotes[iid].EffectiveUnitPrice(); p <= 0 {
			unpricedIngs = append(unpricedIngs, iid)
		}
	}
	npcPrices, _ := s.resolver.EnsureNPCPrices(ctx, unpricedIngs)

	ev := domain.EvaluateRecipe(*rec, lvl, owned, boundOwned, quotes, npcPrices, domain.DefaultMarketTaxRate)
	return &ev, nil
}

func (s *CraftService) loadOwnedAndBoundMaps(ctx context.Context, charKey string, enabled bool) (owned map[domain.ItemID]int64, boundByBaseName map[string]int64) {
	owned = make(map[domain.ItemID]int64)
	boundByBaseName = make(map[string]int64)
	if !enabled || charKey == "" {
		return owned, boundByBaseName
	}

	agg, err := s.invRepo.GetAggregatedInventory(ctx, charKey)
	if err != nil {
		return owned, boundByBaseName
	}

	var unknown []domain.ItemID
	for id, a := range agg {
		if a.ItemName == "" {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		resolved, _ := s.resolver.ResolveItems(ctx, unknown)
		for id, it := range resolved {
			if a, ok := agg[id]; ok {
				a.ItemName = it.Name
				agg[id] = a
			}
		}
	}

	for id, a := range agg {
		owned[id] = a.TotalCount
		if baseName, isBound := domain.StripBoundSuffix(a.ItemName); isBound && baseName != "" {
			boundByBaseName[strings.ToLower(baseName)] += a.TotalCount
		}
	}
	return owned, boundByBaseName
}
