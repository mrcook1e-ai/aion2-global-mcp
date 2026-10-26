package domain

import (
	"fmt"
	"strings"
)

type (
	ItemID     int64
	RecipeID   int64
	StatID     int
	SkillID    int
	NpcID      int64
	MapID      int
	ServerID   int
	InstanceID int64
)

// EntityKind classifies catalog dictionary entries resolved dynamically by ID (never hardcoded in code).
type EntityKind string

const (
	EntityStat      EntityKind = "stat"
	EntitySkill     EntityKind = "skill"
	EntityNPC       EntityKind = "npc"
	EntityBoss      EntityKind = "boss"
	EntityDungeon   EntityKind = "dungeon"
	EntityNightmare EntityKind = "nightmare"
	EntityBuff      EntityKind = "buff"
	EntityDaevanion EntityKind = "daevanion_node"
)

// IsKinahItem identifies direct Kinah currency item IDs (dropped/shared Kinah, face value 1:1 without AH tax).
func IsKinahItem(id ItemID) bool {
	return id == 930100001 || id == 930100030 || id == 930100031
}

// StripBoundSuffix checks whether an item name carries the bound suffix in RU or EN and returns the base tradable name.
func StripBoundSuffix(name string) (baseName string, isBound bool) {
	trimmed := strings.TrimSpace(name)
	for _, suffix := range []string{" (привяз.)", " (Привяз.)", " (Bound)", " (bound)"} {
		if strings.HasSuffix(trimmed, suffix) {
			return strings.TrimSpace(strings.TrimSuffix(trimmed, suffix)), true
		}
	}
	return trimmed, false
}

// Profession identifies a crafting discipline as published in Questlog's recipe catalog.
type Profession string

const (
	ProfessionBlacksmithing Profession = "blacksmithing"
	ProfessionJewelcrafting Profession = "jewelcrafting"
	ProfessionTailoring     Profession = "tailoring"
	ProfessionAlchemy       Profession = "alchemy"
	ProfessionCooking       Profession = "cooking"
)

// NormalizeProfession maps common aliases (EN/RU) to canonical catalog identifiers without hardcoding item/recipe IDs.
func NormalizeProfession(raw string) Profession {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case s == "jewelcrafting" || s == "jewelry" || strings.HasPrefix(s, "ювел"):
		return ProfessionJewelcrafting
	case s == "blacksmithing" || s == "blacksmith" || s == "smithing" || strings.HasPrefix(s, "кузн") || strings.HasPrefix(s, "оруж"):
		return ProfessionBlacksmithing
	case s == "tailoring" || s == "tailor" || strings.HasPrefix(s, "портн") || strings.HasPrefix(s, "швей") || strings.HasPrefix(s, "брон"):
		return ProfessionTailoring
	case s == "alchemy" || strings.HasPrefix(s, "алхим"):
		return ProfessionAlchemy
	case s == "cooking" || s == "cook" || strings.HasPrefix(s, "кулин") || strings.HasPrefix(s, "еда"):
		return ProfessionCooking
	default:
		return Profession(s)
	}
}

// NormalizeMastery converts in-game UI mastery grade + tier level (e.g. "Профи" / "intermediate" 26)
// into the catalog's continuous internal mastery_level scale (beginner: 1..50, intermediate: 51..100, advanced: 101..150).
func NormalizeMastery(gradeRaw string, level int) (canonicalGrade string, internalLevel int, displayLevel string) {
	g := strings.ToLower(strings.TrimSpace(gradeRaw))
	switch {
	case g == "beginner" || strings.HasPrefix(g, "нович") || strings.HasPrefix(g, "начал"):
		canonicalGrade = "beginner"
	case g == "intermediate" || strings.HasPrefix(g, "проф") || strings.HasPrefix(g, "средн"):
		canonicalGrade = "intermediate"
	case g == "advanced" || g == "expert" || g == "artisan" || strings.HasPrefix(g, "эксперт") || strings.HasPrefix(g, "мастер"):
		canonicalGrade = "advanced"
	}

	internalLevel = max(0, level)
	switch canonicalGrade {
	case "intermediate":
		if internalLevel >= 1 && internalLevel <= 50 {
			internalLevel += 50
		}
	case "advanced":
		if internalLevel >= 1 && internalLevel <= 50 {
			internalLevel += 100
		}
	case "":
		switch {
		case internalLevel >= 101:
			canonicalGrade = "advanced"
		case internalLevel >= 51:
			canonicalGrade = "intermediate"
		case internalLevel >= 1:
			canonicalGrade = "beginner"
		}
	}

	displayLevel = FormatMasteryDisplay(canonicalGrade, internalLevel)
	return canonicalGrade, internalLevel, displayLevel
}

// FormatMasteryDisplay renders both the in-game 1..50 tier rank and the internal continuous level.
func FormatMasteryDisplay(canonicalGrade string, internalLevel int) string {
	switch canonicalGrade {
	case "advanced":
		tierLvl := internalLevel
		if tierLvl > 100 {
			tierLvl -= 100
		}
		return fmt.Sprintf("advanced %d (internal %d)", tierLvl, internalLevel)
	case "intermediate":
		tierLvl := internalLevel
		if tierLvl > 50 {
			tierLvl -= 50
		}
		return fmt.Sprintf("intermediate %d (internal %d)", tierLvl, internalLevel)
	case "beginner":
		return fmt.Sprintf("beginner %d", internalLevel)
	default:
		return fmt.Sprintf("level %d", internalLevel)
	}
}

// NormalizeRaceQualification maps character race numbers/names to recipe qualificationRace ("light" / "dark").
func NormalizeRaceQualification(race any) string {
	switch v := race.(type) {
	case int:
		switch v {
		case 1:
			return "light"
		case 2:
			return "dark"
		}
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		switch s {
		case "1", "elyos", "ely", "light":
			return "light"
		case "2", "asmodian", "asmo", "dark":
			return "dark"
		}
	}
	return ""
}
