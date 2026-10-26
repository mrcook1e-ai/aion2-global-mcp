package domain

import (
	"strings"
	"testing"
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

	// Player already owns all 10 ores in bag!
	owned := map[ItemID]int64{
		3001: 10,
	}

	quotes := map[ItemID]*MarketQuote{
		2001: {ItemID: 2001, Listings: 0, MinPrice: 0, Median7d: 0}, // Untradable normal product
		2002: {ItemID: 2002, Listings: 5, MinPrice: 1_000_000, Median7d: 950_000},
		3001: {ItemID: 3001, Listings: 50, MinPrice: 20_000, Median7d: 20_000}, // 10 * 20k = 200k owned value!
		3002: {ItemID: 3002, Listings: 10, MinPrice: 15_000, Median7d: 15_000}, // 2 * 15k = 30k to buy
	}

	ev := EvaluateRecipe(r, 55, owned, quotes, 0.12)

	// FullMaterialCost = 10k fee + 200k + 30k = 240,000
	if ev.FullMaterialCost != 240_000 {
		t.Fatalf("FullMaterialCost = %d, want 240000", ev.FullMaterialCost)
	}
	// OutOfPocketCost = 10k fee + 30k missing = 40,000
	if ev.OutOfPocketCost != 40_000 {
		t.Fatalf("OutOfPocketCost = %d, want 40000", ev.OutOfPocketCost)
	}
	// ExpectedGrossValue = 0.80 * (0.25 * 1,000,000) = 200,000
	if ev.ExpectedGrossValue != 200_000 {
		t.Fatalf("ExpectedGrossValue = %d, want 200000", ev.ExpectedGrossValue)
	}
	// ExpectedNetRevenue = 200,000 * 0.88 = 176,000
	if ev.ExpectedNetRevenue != 176_000 {
		t.Fatalf("ExpectedNetRevenue = %d, want 176000", ev.ExpectedNetRevenue)
	}
	// NetProfitMarket = 176,000 - 240,000 = -64,000 (loss vs selling raw materials!)
	// CashFlowProfit  = 176,000 - 40,000  = +136,000 (illusion of profit!)
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
