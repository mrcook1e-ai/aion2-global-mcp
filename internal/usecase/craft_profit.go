package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

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
	MultiStep       bool    // Evaluate intermediate craftable materials ("craft vs buy") recursively
	SortBy          string  // "net_profit" (default), "cash_flow", or "multi_step"
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
	MultiStepEnabled  bool                     `json:"multiStepEnabled,omitempty"`
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

	profLevels := s.loadProfessionLevels(ctx, charKey)

	if q.MasteryLevel != nil {
		_, playerLvl, displayLvl = domain.NormalizeMastery(q.MasteryGrade, *q.MasteryLevel)
		profLevels[prof] = playerLvl
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
		profLevels[prof] = playerLvl
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

	multiStep := q.MultiStep || q.SortBy == "multi_step"
	var subRecipesByItem map[domain.ItemID]domain.Recipe
	if multiStep {
		subRecipesByItem = s.collectSubRecipes(ctx, recipes, raceQual, 3)
	}

	owned, boundByName := s.loadOwnedAndBoundMaps(ctx, charKey, q.UseInventory)

	boundOwned := make(map[domain.ItemID]int64)
	var allItemIDs []domain.ItemID
	var allIngIDs []domain.ItemID
	matchedInv := make(map[domain.ItemID]struct{})

	collectFromRecipe := func(r domain.Recipe) {
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

	for _, r := range recipes {
		collectFromRecipe(r)
	}
	for _, sr := range subRecipesByItem {
		collectFromRecipe(sr)
	}

	itemsMeta, _ := s.resolver.ResolveItems(ctx, allItemIDs)
	var marketCandidateIDs []domain.ItemID
	for _, id := range deduplicateItemIDs(allItemIDs) {
		it, ok := itemsMeta[id]
		if ok && it.Tradable != nil && !*it.Tradable {
			continue
		}
		marketCandidateIDs = append(marketCandidateIDs, id)
	}

	quotes, err := s.resolver.EnsureMarketQuotes(ctx, serverID, marketCandidateIDs, q.ForceRefresh)
	if err != nil {
		return nil, err
	}

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

	now := time.Now().UTC()
	evals := make([]domain.CraftEvaluation, 0, len(recipes))
	for _, r := range recipes {
		ev := domain.EvaluateRecipeAt(r, playerLvl, owned, boundOwned, quotes, npcPrices, tax, now)
		if ev.ExpectedNetRevenue <= 0 || ev.IncompletePricing {
			continue
		}
		if multiStep {
			domain.AttachSubCrafts(&ev, r, subRecipesByItem, profLevels, playerLvl, owned, boundOwned, quotes, npcPrices, tax, now, 3)
		}
		evals = append(evals, ev)
	}

	sort.Slice(evals, func(i, j int) bool {
		switch q.SortBy {
		case "cash_flow":
			return evals[i].CashFlowProfit > evals[j].CashFlowProfit
		case "multi_step":
			return evals[i].MultiStepNetProfit > evals[j].MultiStepNetProfit
		default:
			if multiStep && evals[i].MultiStepNetProfit != evals[j].MultiStepNetProfit {
				return evals[i].MultiStepNetProfit > evals[j].MultiStepNetProfit
			}
			return evals[i].NetProfitMarket > evals[j].NetProfitMarket
		}
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
		MultiStepEnabled: multiStep,
		RecipesEvaluated: len(recipes),
		InventoryItems:   len(matchedInv),
		TaxRate:          tax,
		TopCrafts:        evals,
	}, nil
}

// EvaluateSingleRecipe calculates full economics (including recursive sub-material craft vs buy!) for one recipe.
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

	var (
		charKey  string
		raceQual string
		srvID    = domain.ServerID(serverID)
	)
	if ch, err := s.charSvc.FindCharacter(ctx, character); err == nil && ch != nil {
		charKey = ch.Key
		raceQual = domain.NormalizeRaceQualification(ch.Race)
		if srvID == 0 {
			srvID = ch.ServerID
		}
	}

	var rec *domain.Recipe
	var err error
	if recipeID > 0 {
		rec, err = s.catalog.GetRecipe(ctx, recipeID)
	} else {
		matches, sErr := s.catalog.SearchRecipes(ctx, domain.RecipeFilter{
			Query:             recipeQuery,
			QualificationRace: raceQual,
			ExcludeGuildCraft: true,
			Limit:             10,
		})
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

	profLevels := s.loadProfessionLevels(ctx, charKey)
	lvl := rec.MasteryLevel
	if savedLvl := profLevels[rec.Profession]; savedLvl > lvl {
		lvl = savedLvl
	}
	if masteryLevel != nil {
		_, lvl, _ = domain.NormalizeMastery(masteryGrade, *masteryLevel)
		profLevels[rec.Profession] = lvl
	}
	if srvID <= 0 {
		return nil, fmt.Errorf("укажи serverId для расчёта рыночных цен")
	}

	subRecipesByItem := s.collectSubRecipes(ctx, []domain.Recipe{*rec}, raceQual, 3)

	owned, boundByName := s.loadOwnedAndBoundMaps(ctx, charKey, true)
	boundOwned := make(map[domain.ItemID]int64)

	var ids []domain.ItemID
	var ingIDs []domain.ItemID
	collect := func(r domain.Recipe) {
		if r.ProductItemID > 0 {
			ids = append(ids, r.ProductItemID)
		}
		if r.ComboItemID > 0 {
			ids = append(ids, r.ComboItemID)
		}
		for _, ing := range r.Ingredients {
			ids = append(ids, ing.ItemID)
			ingIDs = append(ingIDs, ing.ItemID)
			if baseKey := strings.ToLower(strings.TrimSpace(ing.ItemName)); baseKey != "" {
				if bCnt := boundByName[baseKey]; bCnt > 0 {
					boundOwned[ing.ItemID] = bCnt
				}
			}
		}
	}
	collect(*rec)
	for _, sr := range subRecipesByItem {
		collect(sr)
	}

	itemsMeta, _ := s.resolver.ResolveItems(ctx, ids)
	var marketIDs []domain.ItemID
	for _, id := range deduplicateItemIDs(ids) {
		if it, ok := itemsMeta[id]; ok && it.Tradable != nil && !*it.Tradable {
			continue
		}
		marketIDs = append(marketIDs, id)
	}

	quotes, err := s.resolver.EnsureMarketQuotes(ctx, srvID, marketIDs, forceRefresh)
	if err != nil {
		return nil, err
	}
	var unpricedIngs []domain.ItemID
	for _, iid := range deduplicateItemIDs(ingIDs) {
		if p, _ := quotes[iid].EffectiveUnitPrice(); p <= 0 {
			unpricedIngs = append(unpricedIngs, iid)
		}
	}
	npcPrices, _ := s.resolver.EnsureNPCPrices(ctx, unpricedIngs)

	now := time.Now().UTC()
	ev := domain.EvaluateRecipeAt(*rec, lvl, owned, boundOwned, quotes, npcPrices, domain.DefaultMarketTaxRate, now)
	domain.AttachSubCrafts(&ev, *rec, subRecipesByItem, profLevels, lvl, owned, boundOwned, quotes, npcPrices, domain.DefaultMarketTaxRate, now, 3)
	return &ev, nil
}

func (s *CraftService) loadProfessionLevels(ctx context.Context, charKey string) map[domain.Profession]int {
	out := make(map[domain.Profession]int)
	if charKey == "" {
		return out
	}
	profs, err := s.profRepo.GetProfessions(ctx, charKey)
	if err != nil {
		return out
	}
	for _, p := range profs {
		out[p.Profession] = p.MasteryLevel
	}
	return out
}

// collectSubRecipes discovers race-compatible crafting recipes for intermediate craftable ingredients up to maxDepth.
func (s *CraftService) collectSubRecipes(ctx context.Context, roots []domain.Recipe, raceQual string, maxDepth int) map[domain.ItemID]domain.Recipe {
	byItem := make(map[domain.ItemID]domain.Recipe)
	frontier := roots

	for d := 0; d < maxDepth && len(frontier) > 0; d++ {
		var nextFrontier []domain.Recipe
		for _, r := range frontier {
			for _, ing := range r.Ingredients {
				if ing.ItemID <= 0 {
					continue
				}
				if _, seen := byItem[ing.ItemID]; seen {
					continue
				}
				// Check both ProductItemID recipes (preferring character's race!) and explicit CraftableRecipe pointer
				candidates, _ := s.catalog.GetRecipesForItem(ctx, ing.ItemID)
				var chosen *domain.Recipe
				for i := range candidates {
					c := &candidates[i]
					if c.ProductItemID != ing.ItemID || c.IsGuildCraft || len(c.Ingredients) == 0 {
						continue
					}
					if raceQual != "" && c.QualificationRace != "" && !strings.EqualFold(c.QualificationRace, raceQual) {
						continue
					}
					// Prefer same profession as parent recipe when multiple exist
					if chosen == nil || (c.Profession == r.Profession && chosen.Profession != r.Profession) {
						chosen = c
					}
				}
				if chosen == nil && ing.CraftableRecipe > 0 {
					if cr, err := s.catalog.GetRecipe(ctx, ing.CraftableRecipe); err == nil && cr != nil && !cr.IsGuildCraft && len(cr.Ingredients) > 0 {
						if raceQual == "" || cr.QualificationRace == "" || strings.EqualFold(cr.QualificationRace, raceQual) {
							chosen = cr
						}
					}
				}
				if chosen != nil {
					byItem[ing.ItemID] = *chosen
					nextFrontier = append(nextFrontier, *chosen)
				}
			}
		}
		frontier = nextFrontier
	}
	return byItem
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
