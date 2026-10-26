package domain

import "time"

// Item represents a game item cached in SQLite from Questlog's localized catalog.
type Item struct {
	ID           ItemID    `json:"id"`
	Locale       string    `json:"locale"`
	Name         string    `json:"name"`
	Grade        int       `json:"grade"`
	MainCategory string    `json:"mainCategory,omitempty"`
	SubCategory  string    `json:"subCategory,omitempty"`
	Icon         string    `json:"icon,omitempty"`
	ItemLevel    int       `json:"itemLevel,omitempty"`
	LevelReq     int       `json:"levelReq,omitempty"`
	Tradable     *bool     `json:"tradable,omitempty"`
	NPCBuyPrice  int64     `json:"npcBuyPrice,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

// RecipeIngredient is one required input material of a Recipe.
type RecipeIngredient struct {
	RecipeID        RecipeID `json:"recipeId"`
	ItemID          ItemID   `json:"itemId"`
	ItemName        string   `json:"itemName"`
	Quantity        int      `json:"quantity"`
	Grade           int      `json:"grade,omitempty"`
	CraftableRecipe RecipeID `json:"craftableRecipeId,omitempty"`
}

// Recipe represents a crafting recipe with level-dependent success rates and critical (combo) output.
type Recipe struct {
	ID                RecipeID           `json:"id"`
	Locale            string             `json:"locale"`
	Name              string             `json:"name"`
	Grade             int                `json:"grade"`
	Profession        Profession         `json:"profession"` // mainCategory in Questlog
	SubCategory       string             `json:"subCategory,omitempty"`
	QualificationRace string             `json:"qualificationRace,omitempty"` // "", "light", "dark"
	MasteryGrade      string             `json:"masteryGrade,omitempty"`      // "", "beginner", "intermediate", ...
	MasteryLevel      int                `json:"masteryLevel"`
	GoldCost          int64              `json:"goldCost"`
	IsGuildCraft      bool               `json:"isGuildCraft"`
	SuccessRates      []float64          `json:"successRates,omitempty"` // indexed by (playerLevel - MasteryLevel)
	ProductItemID     ItemID             `json:"productItemId"`
	ProductItemName   string             `json:"productItemName"`
	ProductQuantity   int                `json:"productQuantity"`
	ComboItemID       ItemID             `json:"comboItemId,omitempty"`
	ComboItemName     string             `json:"comboItemName,omitempty"`
	ComboProbability  float64            `json:"comboProbability"` // percentage [0..100], e.g. 25.0 = 25%
	Ingredients       []RecipeIngredient `json:"ingredients,omitempty"`
}

// SuccessRateAt returns the exact craft success probability in [0.0, 1.0] at a given player mastery level.
func (r Recipe) SuccessRateAt(playerLevel int) float64 {
	if playerLevel < r.MasteryLevel {
		return 0
	}
	if len(r.SuccessRates) == 0 {
		return 1.0
	}
	idx := playerLevel - r.MasteryLevel
	if idx < 0 {
		idx = 0
	}
	if idx >= len(r.SuccessRates) {
		idx = len(r.SuccessRates) - 1
	}
	rate := r.SuccessRates[idx]
	if rate <= 0 {
		return 0
	}
	if rate > 1.0 {
		// Defensive: in case an upstream source ever switches to 0..100 percentages
		return rate / 100.0
	}
	return rate
}

// ComboProbFraction returns the critical/combo probability in [0.0, 1.0] given a successful craft.
func (r Recipe) ComboProbFraction() float64 {
	if r.ComboItemID == 0 || r.ComboProbability <= 0 {
		return 0
	}
	if r.ComboProbability > 1.0 {
		return min(1.0, r.ComboProbability/100.0)
	}
	return r.ComboProbability
}

// EntityEntry is a generic dictionary record cached in SQLite for stats, skills, NPCs, bosses, maps, etc.
type EntityEntry struct {
	Kind      EntityKind `json:"kind"`
	ID        int64      `json:"id"`
	Locale    string     `json:"locale"`
	Name      string     `json:"name"`
	Category  string     `json:"category,omitempty"`
	MetaJSON  string     `json:"metaJson,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt,omitempty"`
}
