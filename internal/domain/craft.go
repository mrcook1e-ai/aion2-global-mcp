package domain

import (
	"fmt"
	"math"
	"time"
)

// DefaultMarketTaxRate is the standard AION 2 auction house fee+tax rate (12% default, configurable per query).
const DefaultMarketTaxRate = 0.12

// SubCraftComparison compares buying an intermediate craftable ingredient on AH vs crafting it yourself.
type SubCraftComparison struct {
	RecipeID                 RecipeID   `json:"recipeId"`
	RecipeName               string     `json:"recipeName"`
	Profession               Profession `json:"profession"`
	MasteryLevelReq          int        `json:"masteryLevelReq"`
	PlayerMasteryLevel       int        `json:"playerMasteryLevel"`
	CanCraft                 bool       `json:"canCraft"`
	SuccessRate              float64    `json:"successRate"`
	BuyUnitCost              int64      `json:"buyUnitCost"`
	ExpectedCraftUnitCost    int64      `json:"expectedCraftUnitCost"`
	OutOfPocketCraftUnitCost int64      `json:"outOfPocketCraftUnitCost"`
	Recommendation           string     `json:"recommendation"` // "craft", "buy", or "level_locked"
	SavingsTotal             int64      `json:"savingsTotal,omitempty"`
}

// IngredientCostLine breaks down one recipe material against player inventory (tradable + bound) and market/NPC prices.
type IngredientCostLine struct {
	ItemID            ItemID              `json:"itemId"`
	Name              string              `json:"name"`
	RequiredQty       int64               `json:"requiredQty"`
	OwnedQty          int64               `json:"ownedQty"`                // total usable owned (tradable + bound)
	BoundOwnedQty     int64               `json:"boundOwnedQty,omitempty"` // portion of OwnedQty from "(привяз.)" bound equivalents
	UsedOwnedQty      int64               `json:"usedOwnedQty"`
	MissingToBuyQty   int64               `json:"missingToBuyQty"`
	UnitPrice         int64               `json:"unitPrice"`
	PriceSource       string              `json:"priceSource,omitempty"` // "market", "median7d", or "npc"
	ActiveListings    int                 `json:"activeListings"`
	QuoteAgeMinutes   int                 `json:"quoteAgeMinutes"`
	StaleQuote        bool                `json:"staleQuote,omitempty"`
	SpreadVsMedianPct float64             `json:"spreadVsMedianPct,omitempty"`
	FullMarketCost    int64               `json:"fullMarketCost"`   // RequiredQty * UnitPrice (opportunity cost included)
	OutOfPocketCost   int64               `json:"outOfPocketCost"`  // MissingToBuyQty * UnitPrice (actual Kinah to spend now)
	MissingPriceData  bool                `json:"missingPriceData"` // true when neither market nor NPC price exists
	ThinSupply        bool                `json:"thinSupply"`       // true when active market listings < missing quantity to buy
	SubCraft          *SubCraftComparison `json:"subCraft,omitempty"`
}

// CraftEvaluation holds the full economic calculation of one recipe for a specific player level and inventory.
type CraftEvaluation struct {
	RecipeID               RecipeID             `json:"recipeId"`
	RecipeName             string               `json:"recipeName"`
	Profession             Profession           `json:"profession"`
	SubCategory            string               `json:"subCategory,omitempty"`
	MasteryLevelReq        int                  `json:"masteryLevelReq"`
	PlayerLevel            int                  `json:"playerLevel"`
	SuccessRate            float64              `json:"successRate"`      // [0..1]
	ComboProbability       float64              `json:"comboProbability"` // [0..1] conditional on success
	GoldFee                int64                `json:"goldFee"`
	ProductItemID          ItemID               `json:"productItemId"`
	ProductName            string               `json:"productName"`
	ProductQty             int                  `json:"productQty"`
	ProductSellPrice       int64                `json:"productSellPrice"`
	ProductListings        int                  `json:"productListings"`
	ProductQuoteAgeMinutes int                  `json:"productQuoteAgeMinutes"`
	ComboItemID            ItemID               `json:"comboItemId,omitempty"`
	ComboName              string               `json:"comboName,omitempty"`
	ComboSellPrice         int64                `json:"comboSellPrice,omitempty"`
	ComboListings          int                  `json:"comboListings,omitempty"`
	ComboQuoteAgeMinutes   int                  `json:"comboQuoteAgeMinutes,omitempty"`
	StaleQuote             bool                 `json:"staleQuote,omitempty"`
	FullMaterialCost       int64                `json:"fullMaterialCost"`             // GoldFee + sum(Required * UnitPrice)
	OutOfPocketCost        int64                `json:"outOfPocketCost"`              // GoldFee + sum(Missing * UnitPrice)
	OwnedMaterialsVal      int64                `json:"ownedMaterialsVal"`            // Value of owned materials used
	ExpectedGrossValue     int64                `json:"expectedGrossValue"`           // Before AH tax
	ExpectedNetRevenue     int64                `json:"expectedNetRevenue"`           // After AH tax
	NetProfitMarket        int64                `json:"netProfitMarket"`              // ExpectedNetRevenue - FullMaterialCost (true economic profit!)
	CashFlowProfit         int64                `json:"cashFlowProfit"`               // ExpectedNetRevenue - OutOfPocketCost
	MultiStepSavings       int64                `json:"multiStepSavings,omitempty"`   // Extra Kinah saved by crafting profitable sub-materials yourself
	MultiStepNetProfit     int64                `json:"multiStepNetProfit,omitempty"` // NetProfitMarket + MultiStepSavings
	MarginPercent          float64              `json:"marginPercent"`                // NetProfitMarket / FullMaterialCost * 100
	CraftableFromOwned     int64                `json:"craftableFromOwned"`           // Full attempts covered by owned (tradable + bound) items
	MaterialsCoverage      float64              `json:"materialsCoverage"`            // Share of material value already in inventory [0..100%]
	IncompletePricing      bool                 `json:"incompletePricing"`            // True if recipe lacks ingredients or prices
	Warnings               []string             `json:"warnings,omitempty"`
	Ingredients            []IngredientCostLine `json:"ingredients"`
}

// EvaluateRecipe computes true expected value, opportunity-adjusted profit, and out-of-pocket cash flow.
func EvaluateRecipe(
	r Recipe,
	playerLevel int,
	owned map[ItemID]int64,
	boundOwned map[ItemID]int64,
	quotes map[ItemID]*MarketQuote,
	npcPrices map[ItemID]int64,
	taxRate float64,
) CraftEvaluation {
	return EvaluateRecipeAt(r, playerLevel, owned, boundOwned, quotes, npcPrices, taxRate, time.Now().UTC())
}

// EvaluateRecipeAt is identical to EvaluateRecipe with an explicit reference clock for quote freshness checks.
func EvaluateRecipeAt(
	r Recipe,
	playerLevel int,
	owned map[ItemID]int64,
	boundOwned map[ItemID]int64,
	quotes map[ItemID]*MarketQuote,
	npcPrices map[ItemID]int64,
	taxRate float64,
	now time.Time,
) CraftEvaluation {
	if taxRate < 0 || taxRate >= 1 {
		taxRate = DefaultMarketTaxRate
	}

	successRate := r.SuccessRateAt(playerLevel)
	comboProb := r.ComboProbFraction()
	prodQty := max(1, r.ProductQuantity)

	eval := CraftEvaluation{
		RecipeID:         r.ID,
		RecipeName:       r.Name,
		Profession:       r.Profession,
		SubCategory:      r.SubCategory,
		MasteryLevelReq:  r.MasteryLevel,
		PlayerLevel:      playerLevel,
		SuccessRate:      math.Round(successRate*10000) / 10000,
		ComboProbability: math.Round(comboProb*10000) / 10000,
		GoldFee:          r.GoldCost,
		ProductItemID:    r.ProductItemID,
		ProductName:      r.ProductItemName,
		ProductQty:       prodQty,
		ComboItemID:      r.ComboItemID,
		ComboName:        r.ComboItemName,
		FullMaterialCost: r.GoldCost,
		OutOfPocketCost:  r.GoldCost,
		Ingredients:      make([]IngredientCostLine, 0, len(r.Ingredients)),
	}

	maxCraftsFromOwned := int64(math.MaxInt64)
	if len(r.Ingredients) == 0 {
		maxCraftsFromOwned = 0
		eval.IncompletePricing = true
		eval.Warnings = append(eval.Warnings, "в каталоге у рецепта отсутствуют ингредиенты")
	}

	for _, ing := range r.Ingredients {
		reqQty := int64(max(1, ing.Quantity))
		tradableHave := owned[ing.ItemID]
		boundHave := boundOwned[ing.ItemID]
		haveQty := tradableHave + boundHave
		usedOwned := min(reqQty, haveQty)
		missingQty := reqQty - usedOwned

		if craftsForIng := haveQty / reqQty; craftsForIng < maxCraftsFromOwned {
			maxCraftsFromOwned = craftsForIng
		}

		q := quotes[ing.ItemID]
		unitPrice, active := q.EffectiveUnitPrice()
		listings := 0
		priceSource := ""
		var quoteAge int
		var stale bool
		var spreadPct float64
		if q != nil {
			qCopy := *q
			qCopy.HydrateFreshness(now)
			listings = qCopy.Listings
			quoteAge = qCopy.QuoteAgeMinutes
			stale = qCopy.StaleQuote
			spreadPct = qCopy.SpreadVsMedianPct()
		}
		if unitPrice > 0 {
			if active {
				priceSource = "market"
			} else {
				priceSource = "median7d"
			}
		} else if npcP := npcPrices[ing.ItemID]; npcP > 0 {
			unitPrice = npcP
			priceSource = "npc"
			quoteAge = 0
			stale = false
			spreadPct = 0
		}

		line := IngredientCostLine{
			ItemID:            ing.ItemID,
			Name:              ing.ItemName,
			RequiredQty:       reqQty,
			OwnedQty:          haveQty,
			BoundOwnedQty:     boundHave,
			UsedOwnedQty:      usedOwned,
			MissingToBuyQty:   missingQty,
			UnitPrice:         unitPrice,
			PriceSource:       priceSource,
			ActiveListings:    listings,
			QuoteAgeMinutes:   quoteAge,
			StaleQuote:        stale,
			SpreadVsMedianPct: spreadPct,
		}

		if unitPrice > 0 {
			line.FullMarketCost = reqQty * unitPrice
			line.OutOfPocketCost = missingQty * unitPrice
			eval.FullMaterialCost += line.FullMarketCost
			eval.OutOfPocketCost += line.OutOfPocketCost
			eval.OwnedMaterialsVal += usedOwned * unitPrice
			if priceSource != "npc" && missingQty > 0 && (!active || int64(listings) < missingQty) {
				line.ThinSupply = true
				eval.Warnings = append(eval.Warnings, "мало лотов ингредиента на аукционе: "+ing.ItemName)
			}
			if priceSource == "market" && q != nil && q.HasSlippageRisk() {
				eval.Warnings = append(eval.Warnings, fmt.Sprintf("разрыв minPrice vs median7d %.1f%% у ингредиента %s (риск проскальзывания стакана)", spreadPct, ing.ItemName))
			}
		} else {
			line.MissingPriceData = true
			eval.IncompletePricing = true
			if missingQty > 0 {
				eval.Warnings = append(eval.Warnings, "нет цены на недостающий ингредиент: "+ing.ItemName)
			} else {
				eval.Warnings = append(eval.Warnings, "ингредиент есть в сумке, но не торгуется/нет цены: "+ing.ItemName)
			}
		}

		eval.Ingredients = append(eval.Ingredients, line)
	}

	// Flag stale quotes on expensive ingredients (>= 15% of full material cost)
	for _, line := range eval.Ingredients {
		if line.StaleQuote && eval.FullMaterialCost > 0 && float64(line.FullMarketCost)/float64(eval.FullMaterialCost) >= 0.15 {
			eval.StaleQuote = true
			eval.Warnings = append(eval.Warnings, fmt.Sprintf("устаревшая котировка (%d мин) у ключевого ингредиента: %s", line.QuoteAgeMinutes, line.Name))
		}
	}

	if maxCraftsFromOwned == math.MaxInt64 {
		maxCraftsFromOwned = 0
	}
	eval.CraftableFromOwned = maxCraftsFromOwned

	matOnlyFull := eval.FullMaterialCost - r.GoldCost
	if matOnlyFull > 0 {
		eval.MaterialsCoverage = math.Round(float64(eval.OwnedMaterialsVal)/float64(matOnlyFull)*1000) / 10
	}

	prodQuote := quotes[r.ProductItemID]
	prodPrice, prodCapped, hasProdPrice := prodQuote.ConservativeSellPrice()
	if prodQuote != nil {
		pq := *prodQuote
		pq.HydrateFreshness(now)
		eval.ProductListings = pq.Listings
		eval.ProductQuoteAgeMinutes = pq.QuoteAgeMinutes
		if hasProdPrice && pq.StaleQuote {
			eval.StaleQuote = true
			eval.Warnings = append(eval.Warnings, fmt.Sprintf("устаревшая котировка обычного продукта (%d мин)", pq.QuoteAgeMinutes))
		}
		if hasProdPrice && !prodCapped && pq.HasSlippageRisk() {
			eval.Warnings = append(eval.Warnings, fmt.Sprintf("разрыв minPrice vs median7d %.1f%% на обычном продукте", pq.SpreadVsMedianPct()))
		}
	}
	eval.ProductSellPrice = prodPrice
	if prodCapped {
		eval.Warnings = append(eval.Warnings, "цена обычного продукта взята по median7d (защита от завышенного лота)")
	}

	var comboPrice int64
	hasComboPrice := false
	if r.ComboItemID != 0 {
		comboQuote := quotes[r.ComboItemID]
		var comboCapped bool
		comboPrice, comboCapped, hasComboPrice = comboQuote.ConservativeSellPrice()
		if comboQuote != nil {
			cq := *comboQuote
			cq.HydrateFreshness(now)
			eval.ComboListings = cq.Listings
			eval.ComboQuoteAgeMinutes = cq.QuoteAgeMinutes
			if hasComboPrice && cq.StaleQuote {
				eval.StaleQuote = true
				eval.Warnings = append(eval.Warnings, fmt.Sprintf("устаревшая котировка крит-продукта (%d мин)", cq.QuoteAgeMinutes))
			}
			if hasComboPrice && !comboCapped && cq.HasSlippageRisk() {
				eval.Warnings = append(eval.Warnings, fmt.Sprintf("разрыв minPrice vs median7d %.1f%% на крит-продукте", cq.SpreadVsMedianPct()))
			}
		}
		eval.ComboSellPrice = comboPrice
		if comboCapped {
			eval.Warnings = append(eval.Warnings, "цена крит-продукта взята по median7d (защита от завышенного лота)")
		}
	}

	if !hasProdPrice && (r.ComboItemID == 0 || !hasComboPrice) {
		eval.IncompletePricing = true
		eval.Warnings = append(eval.Warnings, "нет рыночной цены ни на основной, ни на крит-продукт")
	} else if !hasProdPrice && hasComboPrice {
		eval.Warnings = append(eval.Warnings, "основной продукт не продаётся на ауке — окупаемость только за счёт крита (прока)")
	}

	normalRevenue := float64(prodPrice*int64(prodQty)) * (1.0 - comboProb)
	comboRevenue := float64(comboPrice*int64(prodQty)) * comboProb
	if r.ComboItemID != 0 && !hasComboPrice && hasProdPrice {
		comboRevenue = float64(prodPrice*int64(prodQty)) * comboProb
	}

	gross := successRate * (normalRevenue + comboRevenue)
	net := gross * (1.0 - taxRate)

	eval.ExpectedGrossValue = int64(math.Round(gross))
	eval.ExpectedNetRevenue = int64(math.Round(net))
	eval.NetProfitMarket = eval.ExpectedNetRevenue - eval.FullMaterialCost
	eval.CashFlowProfit = eval.ExpectedNetRevenue - eval.OutOfPocketCost
	eval.MultiStepNetProfit = eval.NetProfitMarket

	if eval.FullMaterialCost > 0 {
		eval.MarginPercent = math.Round(float64(eval.NetProfitMarket)/float64(eval.FullMaterialCost)*1000) / 10
	}

	if eval.NetProfitMarket < 0 && eval.CashFlowProfit > 0 && eval.OwnedMaterialsVal > 0 {
		eval.Warnings = append(eval.Warnings, "ВНИМАНИЕ: крафт убыточен относительно рынка — непривязанные ресурсы выгоднее продать сырьём на аукционе!")
	}

	return eval
}

// AttachSubCrafts evaluates intermediate craftable ingredients recursively (up to maxDepth)
// and computes "craft vs buy" recommendations and multi-step savings.
func AttachSubCrafts(
	eval *CraftEvaluation,
	r Recipe,
	subRecipesByItem map[ItemID]Recipe,
	profLevels map[Profession]int,
	defaultSameProfLvl int,
	owned map[ItemID]int64,
	boundOwned map[ItemID]int64,
	quotes map[ItemID]*MarketQuote,
	npcPrices map[ItemID]int64,
	taxRate float64,
	now time.Time,
	maxDepth int,
) {
	if eval == nil || maxDepth <= 0 || len(subRecipesByItem) == 0 {
		return
	}
	visited := map[RecipeID]bool{r.ID: true}
	attachSubCraftsRec(eval, r, subRecipesByItem, profLevels, defaultSameProfLvl, owned, boundOwned, quotes, npcPrices, taxRate, now, maxDepth, visited)
}

func attachSubCraftsRec(
	eval *CraftEvaluation,
	r Recipe,
	subRecipesByItem map[ItemID]Recipe,
	profLevels map[Profession]int,
	defaultSameProfLvl int,
	owned map[ItemID]int64,
	boundOwned map[ItemID]int64,
	quotes map[ItemID]*MarketQuote,
	npcPrices map[ItemID]int64,
	taxRate float64,
	now time.Time,
	depth int,
	visited map[RecipeID]bool,
) {
	if depth <= 0 {
		return
	}
	var totalSavings int64

	for i := range eval.Ingredients {
		line := &eval.Ingredients[i]
		subRec, ok := subRecipesByItem[line.ItemID]
		if !ok || len(subRec.Ingredients) == 0 || visited[subRec.ID] {
			continue
		}

		subLvl := profLevels[subRec.Profession]
		if subLvl == 0 && subRec.Profession == r.Profession {
			subLvl = defaultSameProfLvl
		}

		evalLvl := subLvl
		canCraft := subLvl >= subRec.MasteryLevel
		if !canCraft {
			evalLvl = subRec.MasteryLevel
		}

		nextVisited := make(map[RecipeID]bool, len(visited)+1)
		for k, v := range visited {
			nextVisited[k] = v
		}
		nextVisited[subRec.ID] = true

		subEval := EvaluateRecipeAt(subRec, evalLvl, owned, boundOwned, quotes, npcPrices, taxRate, now)
		if subEval.IncompletePricing {
			continue
		}
		if depth > 1 {
			attachSubCraftsRec(&subEval, subRec, subRecipesByItem, profLevels, subLvl, owned, boundOwned, quotes, npcPrices, taxRate, now, depth-1, nextVisited)
		}

		effectiveAttemptMarketCost := subEval.FullMaterialCost - subEval.MultiStepSavings
		if effectiveAttemptMarketCost < 0 {
			effectiveAttemptMarketCost = 0
		}

		sr := subEval.SuccessRate
		if sr <= 0 {
			continue
		}

		prodQty := float64(max(1, subRec.ProductQuantity))
		comboProb := subRec.ComboProbFraction()

		expectedUnits := sr * prodQty
		var sideComboNetCredit float64
		if subRec.ComboItemID != 0 && subRec.ComboItemID != subRec.ProductItemID && comboProb > 0 {
			if subEval.ComboSellPrice > 0 {
				expectedUnits = sr * (1.0 - comboProb) * prodQty
				sideComboNetCredit = sr * comboProb * float64(subEval.ComboSellPrice) * prodQty * (1.0 - taxRate)
			}
		}
		if expectedUnits <= 0 {
			continue
		}

		netAttemptMarket := float64(effectiveAttemptMarketCost) - sideComboNetCredit
		if netAttemptMarket < 0 {
			netAttemptMarket = 0
		}
		expectedCraftUnit := int64(math.Round(netAttemptMarket / expectedUnits))

		netAttemptOOP := float64(subEval.OutOfPocketCost) - sideComboNetCredit
		if netAttemptOOP < 0 {
			netAttemptOOP = 0
		}
		oopCraftUnit := int64(math.Round(netAttemptOOP / expectedUnits))

		recStr := "buy"
		var savings int64
		if !canCraft {
			recStr = "level_locked"
		} else if line.UnitPrice > 0 && expectedCraftUnit < line.UnitPrice {
			recStr = "craft"
			savings = (line.UnitPrice - expectedCraftUnit) * line.RequiredQty
			totalSavings += savings
		}

		line.SubCraft = &SubCraftComparison{
			RecipeID:                 subRec.ID,
			RecipeName:               subRec.Name,
			Profession:               subRec.Profession,
			MasteryLevelReq:          subRec.MasteryLevel,
			PlayerMasteryLevel:       subLvl,
			CanCraft:                 canCraft,
			SuccessRate:              sr,
			BuyUnitCost:              line.UnitPrice,
			ExpectedCraftUnitCost:    expectedCraftUnit,
			OutOfPocketCraftUnitCost: oopCraftUnit,
			Recommendation:           recStr,
			SavingsTotal:             savings,
		}
	}

	eval.MultiStepSavings = totalSavings
	eval.MultiStepNetProfit = eval.NetProfitMarket + totalSavings
}
