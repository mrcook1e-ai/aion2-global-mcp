package domain

import (
	"strings"
	"testing"
	"time"
)

func TestSuccessRateAt(t *testing.T) {
	r := Recipe{
		MasteryLevel: 55,
		SuccessRates: []float64{0.6751, 0.70, 0.75},
	}
	if got := r.SuccessRateAt(54); got != 0 {
		t.Fatalf("below mastery level: got %v, want 0", got)
	}
	if got := r.SuccessRateAt(55); got != 0.6751 {
		t.Fatalf("at 55: got %v, want 0.6751", got)
	}
	if got := r.SuccessRateAt(56); got != 0.70 {
		t.Fatalf("at 56: got %v, want 0.70", got)
	}
	if got := r.SuccessRateAt(99); got != 0.75 {
		t.Fatalf("above table cap: got %v, want 0.75", got)
	}
}

func TestNormalizeMastery(t *testing.T) {
	grade, lvl, disp := NormalizeMastery("Профи", 26)
	if grade != "intermediate" || lvl != 76 {
		t.Fatalf("Профи 26: got grade=%q lvl=%d disp=%q, want intermediate & 76", grade, lvl, disp)
	}
	grade2, lvl2, _ := NormalizeMastery("intermediate", 76)
	if grade2 != "intermediate" || lvl2 != 76 {
		t.Fatalf("already normalized 76: got grade=%q lvl=%d", grade2, lvl2)
	}
	grade3, lvl3, _ := NormalizeMastery("новичок", 30)
	if grade3 != "beginner" || lvl3 != 30 {
		t.Fatalf("новичок 30: got grade=%q lvl=%d", grade3, lvl3)
	}
}

func TestEvaluateRecipe_OpportunityCostWarningAndComboOnly(t *testing.T) {
	r := Recipe{
		ID:               111013001,
		Name:             "Кольцо истинных драконов",
		Profession:       ProfessionJewelcrafting,
		MasteryLevel:     55,
		SuccessRates:     []float64{0.80}, // 80% success at level 55
		GoldCost:         10_000,
		ProductItemID:    2001, // Soulbound non-tradable normal result
		ProductItemName:  "Кольцо дракона",
		ProductQuantity:  1,
		ComboItemID:      2002, // Tradable shiny crit result
		ComboItemName:    "Блестящее кольцо дракона",
		ComboProbability: 25.0, // 25% crit
		Ingredients: []RecipeIngredient{
			{RecipeID: 111013001, ItemID: 3001, ItemName: "Сияющая руда", Quantity: 10},
			{RecipeID: 111013001, ItemID: 3002, ItemName: "Плавильный камень", Quantity: 2},
		},
	}

	owned := map[ItemID]int64{3001: 10}
	quotes := map[ItemID]*MarketQuote{
		2001: {ItemID: 2001, Listings: 0, MinPrice: 0, Median7d: 0},
		2002: {ItemID: 2002, Listings: 5, MinPrice: 1_000_000, Median7d: 950_000},
		3001: {ItemID: 3001, Listings: 50, MinPrice: 20_000, Median7d: 20_000},
		3002: {ItemID: 3002, Listings: 10, MinPrice: 15_000, Median7d: 15_000},
	}

	ev := EvaluateRecipe(r, 55, owned, nil, quotes, nil, 0.12)

	if ev.FullMaterialCost != 240_000 {
		t.Fatalf("FullMaterialCost = %d, want 240000", ev.FullMaterialCost)
	}
	if ev.OutOfPocketCost != 40_000 {
		t.Fatalf("OutOfPocketCost = %d, want 40000", ev.OutOfPocketCost)
	}
	if ev.ExpectedGrossValue != 200_000 || ev.ExpectedNetRevenue != 176_000 {
		t.Fatalf("revenue mismatch: gross=%d net=%d", ev.ExpectedGrossValue, ev.ExpectedNetRevenue)
	}
	if ev.NetProfitMarket != -64_000 || ev.CashFlowProfit != 136_000 {
		t.Fatalf("profits mismatch: net=%d cash=%d", ev.NetProfitMarket, ev.CashFlowProfit)
	}

	foundOpportunityWarn := false
	for _, w := range ev.Warnings {
		if strings.Contains(w, "выгоднее продать сырьём") {
			foundOpportunityWarn = true
		}
	}
	if !foundOpportunityWarn {
		t.Fatalf("expected opportunity-cost warning, got warnings: %v", ev.Warnings)
	}
}

func TestEvaluateRecipe_BoundMaterialsAndNPCPrice(t *testing.T) {
	r := Recipe{
		ID:              225024001,
		Name:            "Усиленная толстая чешуя балаура",
		Profession:      ProfessionJewelcrafting,
		MasteryLevel:    1,
		ProductItemID:   610730022,
		ProductItemName: "Усиленная толстая чешуя балаура",
		ProductQuantity: 1,
		Ingredients: []RecipeIngredient{
			{RecipeID: 225024001, ItemID: 610610015, ItemName: "Чистый эфир", Quantity: 10},
			{RecipeID: 225024001, ItemID: 630520007, ItemName: "Простой усилитель чешуи дракона (привяз.)", Quantity: 1},
		},
	}

	owned := map[ItemID]int64{610610015: 4}
	boundOwned := map[ItemID]int64{610610015: 6} // 4 tradable + 6 bound = 10 total!
	quotes := map[ItemID]*MarketQuote{
		610730022: {ItemID: 610730022, Listings: 14, MinPrice: 45_000, Median7d: 50_000},
		610610015: {ItemID: 610610015, Listings: 40, MinPrice: 2_000, Median7d: 2_000},
	}
	npcPrices := map[ItemID]int64{
		630520007: 4_650,
	}

	ev := EvaluateRecipe(r, 76, owned, boundOwned, quotes, npcPrices, 0.12)
	if ev.IncompletePricing {
		t.Fatalf("expected complete pricing with NPC price, got warnings: %v", ev.Warnings)
	}
	// FullMaterialCost = 10*2000 + 1*4650 = 24,650
	if ev.FullMaterialCost != 24_650 {
		t.Fatalf("FullMaterialCost = %d, want 24650", ev.FullMaterialCost)
	}
	// OutOfPocketCost = 0 missing ether + 4650 NPC enhancer = 4,650
	if ev.OutOfPocketCost != 4_650 {
		t.Fatalf("OutOfPocketCost = %d, want 4650", ev.OutOfPocketCost)
	}
	if ev.Ingredients[0].BoundOwnedQty != 6 || ev.Ingredients[1].PriceSource != "npc" {
		t.Fatalf("unexpected ingredient details: %+v", ev.Ingredients)
	}
}

func TestAttachSubCrafts_RecursiveCraftVsBuyAndFreshness(t *testing.T) {
	now := time.Now().UTC()
	staleTs := now.Add(-3 * time.Hour) // 180 min -> staleQuote: true

	parent := Recipe{
		ID:              213013001,
		Name:            "Ожерелье Короля неборожденных драконов",
		Profession:      ProfessionJewelcrafting,
		MasteryLevel:    75,
		SuccessRates:    []float64{1.0},
		ProductItemID:   9001,
		ProductItemName: "Ожерелье Короля",
		ProductQuantity: 1,
		Ingredients: []RecipeIngredient{
			{RecipeID: 213013001, ItemID: 610730022, ItemName: "Усиленная толстая чешуя балаура", Quantity: 10, CraftableRecipe: 225024001},
		},
	}

	subRec := Recipe{
		ID:              225024001,
		Name:            "Усиленная толстая чешуя балаура",
		Profession:      ProfessionJewelcrafting,
		MasteryLevel:    25,
		SuccessRates:    []float64{0.80}, // 80% success -> expectedCraftUnitCost = 20,000 / 0.8 = 25,000 (< 40,000 buy!)
		ProductItemID:   610730022,
		ProductItemName: "Усиленная толстая чешуя балаура",
		ProductQuantity: 1,
		Ingredients: []RecipeIngredient{
			{RecipeID: 225024001, ItemID: 610610015, ItemName: "Чистый эфир", Quantity: 10},
		},
	}

	quotes := map[ItemID]*MarketQuote{
		9001:      {ItemID: 9001, Listings: 3, MinPrice: 600_000, Median7d: 600_000, UpdatedAt: staleTs},
		610730022: {ItemID: 610730022, Listings: 20, MinPrice: 40_000, Median7d: 28_000, UpdatedAt: now.Add(-10 * time.Minute)},
		610610015: {ItemID: 610610015, Listings: 100, MinPrice: 2_000, Median7d: 2_000, UpdatedAt: now.Add(-5 * time.Minute)},
	}

	ev := EvaluateRecipeAt(parent, 76, nil, nil, quotes, nil, 0.12, now)
	if !ev.StaleQuote || ev.ProductQuoteAgeMinutes != 180 {
		t.Fatalf("expected stale product quote (180m), got stale=%v age=%d", ev.StaleQuote, ev.ProductQuoteAgeMinutes)
	}

	AttachSubCrafts(
		&ev,
		parent,
		map[ItemID]Recipe{610730022: subRec},
		map[Profession]int{ProfessionJewelcrafting: 76},
		76,
		nil,
		nil,
		quotes,
		nil,
		0.12,
		now,
		3,
	)

	sc := ev.Ingredients[0].SubCraft
	if sc == nil || sc.Recommendation != "craft" {
		t.Fatalf("expected SubCraft recommendation='craft', got %+v", sc)
	}
	if sc.ExpectedCraftUnitCost != 25_000 || sc.SavingsTotal != 150_000 {
		t.Fatalf("unexpected subCraft math: unit=%d savings=%d (want 25000 & 150000)", sc.ExpectedCraftUnitCost, sc.SavingsTotal)
	}
	if ev.MultiStepSavings != 150_000 || ev.MultiStepNetProfit != ev.NetProfitMarket+150_000 {
		t.Fatalf("MultiStepNetProfit mismatch: flat=%d savings=%d multi=%d", ev.NetProfitMarket, ev.MultiStepSavings, ev.MultiStepNetProfit)
	}
}
