package domain

import "math"

// DefaultMarketTaxRate is the standard AION 2 auction house fee+tax rate (12% default, configurable per query).
const DefaultMarketTaxRate = 0.12

// IngredientCostLine breaks down one recipe material against player inventory (tradable + bound) and market/NPC prices.
type IngredientCostLine struct {
	ItemID           ItemID `json:"itemId"`
	Name             string `json:"name"`
	RequiredQty      int64  `json:"requiredQty"`
	OwnedQty         int64  `json:"ownedQty"`                // total usable owned (tradable + bound)
	BoundOwnedQty    int64  `json:"boundOwnedQty,omitempty"` // portion of OwnedQty from "(привяз.)" bound equivalents
	UsedOwnedQty     int64  `json:"usedOwnedQty"`
	MissingToBuyQty  int64  `json:"missingToBuyQty"`
	UnitPrice        int64  `json:"unitPrice"`
	PriceSource      string `json:"priceSource,omitempty"` // "market", "median7d", or "npc"
	ActiveListings   int    `json:"activeListings"`
	FullMarketCost   int64  `json:"fullMarketCost"`   // RequiredQty * UnitPrice (opportunity cost included)
	OutOfPocketCost  int64  `json:"outOfPocketCost"`  // MissingToBuyQty * UnitPrice (actual Kinah to spend now)
	MissingPriceData bool   `json:"missingPriceData"` // true when neither market nor NPC price exists
	ThinSupply       bool   `json:"thinSupply"`       // true when active market listings < missing quantity to buy
}

// CraftEvaluation holds the full economic calculation of one recipe for a specific player level and inventory.
type CraftEvaluation struct {
	RecipeID           RecipeID             `json:"recipeId"`
	RecipeName         string               `json:"recipeName"`
	Profession         Profession           `json:"profession"`
	SubCategory        string               `json:"subCategory,omitempty"`
	MasteryLevelReq    int                  `json:"masteryLevelReq"`
	PlayerLevel        int                  `json:"playerLevel"`
	SuccessRate        float64              `json:"successRate"`      // [0..1]
	ComboProbability   float64              `json:"comboProbability"` // [0..1] conditional on success
	GoldFee            int64                `json:"goldFee"`
	ProductItemID      ItemID               `json:"productItemId"`
	ProductName        string               `json:"productName"`
	ProductQty         int                  `json:"productQty"`
	ProductSellPrice   int64                `json:"productSellPrice"`
	ProductListings    int                  `json:"productListings"`
	ComboItemID        ItemID               `json:"comboItemId,omitempty"`
	ComboName          string               `json:"comboName,omitempty"`
	ComboSellPrice     int64                `json:"comboSellPrice,omitempty"`
	ComboListings      int                  `json:"comboListings,omitempty"`
	FullMaterialCost   int64                `json:"fullMaterialCost"`   // GoldFee + sum(Required * UnitPrice)
	OutOfPocketCost    int64                `json:"outOfPocketCost"`    // GoldFee + sum(Missing * UnitPrice)
	OwnedMaterialsVal  int64                `json:"ownedMaterialsVal"`  // Value of owned materials used
	ExpectedGrossValue int64                `json:"expectedGrossValue"` // Before AH tax
	ExpectedNetRevenue int64                `json:"expectedNetRevenue"` // After AH tax
	NetProfitMarket    int64                `json:"netProfitMarket"`    // ExpectedNetRevenue - FullMaterialCost (true economic profit!)
	CashFlowProfit     int64                `json:"cashFlowProfit"`     // ExpectedNetRevenue - OutOfPocketCost
	MarginPercent      float64              `json:"marginPercent"`      // NetProfitMarket / FullMaterialCost * 100
	CraftableFromOwned int64                `json:"craftableFromOwned"` // Full attempts covered by owned (tradable + bound) items
	MaterialsCoverage  float64              `json:"materialsCoverage"`  // Share of material value already in inventory [0..100%]
	IncompletePricing  bool                 `json:"incompletePricing"`  // True if recipe lacks ingredients or prices
	Warnings           []string             `json:"warnings,omitempty"`
	Ingredients        []IngredientCostLine `json:"ingredients"`
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
		if q != nil {
			listings = q.Listings
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
		}

		line := IngredientCostLine{
			ItemID:          ing.ItemID,
			Name:            ing.ItemName,
			RequiredQty:     reqQty,
			OwnedQty:        haveQty,
			BoundOwnedQty:   boundHave,
			UsedOwnedQty:    usedOwned,
			MissingToBuyQty: missingQty,
			UnitPrice:       unitPrice,
			PriceSource:     priceSource,
			ActiveListings:  listings,
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
		eval.ProductListings = prodQuote.Listings
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
			eval.ComboListings = comboQuote.Listings
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

	if eval.FullMaterialCost > 0 {
		eval.MarginPercent = math.Round(float64(eval.NetProfitMarket)/float64(eval.FullMaterialCost)*1000) / 10
	}

	if eval.NetProfitMarket < 0 && eval.CashFlowProfit > 0 && eval.OwnedMaterialsVal > 0 {
		eval.Warnings = append(eval.Warnings, "ВНИМАНИЕ: крафт убыточен относительно рынка — непривязанные ресурсы выгоднее продать сырьём на аукционе!")
	}

	return eval
}
