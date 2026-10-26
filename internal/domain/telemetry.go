package domain

import "time"

// ContainerKind separates character bag stacks from warehouse/storage snapshots captured in .ndjson logs.
type ContainerKind string

const (
	ContainerBag     ContainerKind = "bag"
	ContainerStorage ContainerKind = "storage"
)

// InventoryStack is one item stack tracked by its unique 64-bit game instance ID.
type InventoryStack struct {
	CharacterKey string        `json:"characterKey"`
	Container    ContainerKind `json:"container"`
	InstanceID   InstanceID    `json:"instanceId"`
	ItemID       ItemID        `json:"itemId"`
	ItemName     string        `json:"itemName,omitempty"`
	Count        int64         `json:"count"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

// AggregatedItem summarizes total owned quantity of an ItemID across bag and storage.
type AggregatedItem struct {
	ItemID       ItemID `json:"itemId"`
	ItemName     string `json:"itemName"`
	Grade        int    `json:"grade,omitempty"`
	Category     string `json:"category,omitempty"`
	BagCount     int64  `json:"bagCount"`
	StorageCount int64  `json:"storageCount"`
	TotalCount   int64  `json:"totalCount"`
}

// ProfessionProgress stores a character's crafting mastery level persisted in SQLite.
type ProfessionProgress struct {
	CharacterKey string     `json:"characterKey"`
	Profession   Profession `json:"profession"`
	MasteryGrade string     `json:"masteryGrade,omitempty"`
	MasteryLevel int        `json:"masteryLevel"` // internal 1..100+ scale
	DisplayLevel string     `json:"displayLevel"` // e.g. "intermediate 26 (internal 76)"
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// GearSlot represents one worn item from Questlog's characters.json.
type GearSlot struct {
	Slot         int      `json:"slot"`
	ItemID       ItemID   `json:"itemId"`
	ItemName     string   `json:"itemName,omitempty"`
	Grade        int      `json:"grade,omitempty"`
	ItemLevel    int      `json:"itemLevel,omitempty"`
	EnchantLevel int      `json:"enchantLevel,omitempty"`
	ExceedLevel  int      `json:"exceedLevel,omitempty"`
	ManastoneIDs []ItemID `json:"manastoneIds,omitempty"`
	Manastones   []string `json:"manastones,omitempty"`
}

// StatValue is a resolved character stat.
type StatValue struct {
	StatID StatID  `json:"statId"`
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
}

// CharacterSkill represents an active/passive skill and its selected specializations (tripods).
type CharacterSkill struct {
	SkillID               SkillID  `json:"skillId"`
	Name                  string   `json:"name,omitempty"`
	Level                 int      `json:"level"`
	Equipped              bool     `json:"equipped"`
	Specializations       []int    `json:"specializations,omitempty"`
	SpecializationDetails []string `json:"specializationDetails,omitempty"`
}

// DaevanionBoard summarizes unlocked nodes on one Daevanion board (31..36).
type DaevanionBoard struct {
	BoardID       int   `json:"boardId"`
	UnlockedCount int   `json:"unlockedCount"`
	Nodes         []int `json:"nodes,omitempty"`
}

// AbyssSeasonStats tracks seasonal PvP rank, Abyss Points, and K/D/A from characters.json.
type AbyssSeasonStats struct {
	Grade      int       `json:"grade"`
	Points     int64     `json:"points"`
	Kills      int       `json:"kills"`
	Deaths     int       `json:"deaths"`
	Assists    int       `json:"assists"`
	RankChange int       `json:"rankChange,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
}

// GenusEffect is one rolled stat slot on a Genus (Lineage) branch.
type GenusEffect struct {
	Slot     int     `json:"slot"`
	Grade    int     `json:"grade"`
	StatID   StatID  `json:"statId"`
	StatName string  `json:"statName,omitempty"`
	Value    float64 `json:"value"`
}

// GenusBranch represents one Genus branch (e.g. intellect, feral) and its rolled options.
type GenusBranch struct {
	Name    string        `json:"name"`
	Level   int           `json:"level"`
	Effects []GenusEffect `json:"effects,omitempty"`
}

// MonolithEntry tracks Monolith token progress.
type MonolithEntry struct {
	MonolithID int `json:"monolithId"`
	Tokens     int `json:"tokens"`
}

// PantheonEntry tracks equipped Pantheon relics/decorations.
type PantheonEntry struct {
	Slot   int `json:"slot"`
	DecoID int `json:"decoId"`
}

// WingInfo describes an equipped wing skin/item.
type WingInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name,omitempty"`
}

// TitleRef describes an equipped or owned character title.
type TitleRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name,omitempty"`
}

// PetEntry represents an owned pet/mount and its level.
type PetEntry struct {
	ID    int64  `json:"id"`
	Name  string `json:"name,omitempty"`
	Level int    `json:"level"`
}

// Character represents a local player character tracked by Questlog Desktop.
type Character struct {
	Key                 string               `json:"key"` // e.g. "2319:Cook1e"
	ID                  string               `json:"id,omitempty"`
	Name                string               `json:"name"`
	ServerID            ServerID             `json:"serverId"`
	Class               string               `json:"class"`
	Race                string               `json:"race,omitempty"`
	Level               int                  `json:"level"`
	GearScore           int                  `json:"gearScore,omitempty"`
	CombatPower         int                  `json:"combatPower,omitempty"`
	Legion              string               `json:"legion,omitempty"`
	IsActive            bool                 `json:"isActive,omitempty"`
	UpdatedAt           time.Time            `json:"updatedAt"`
	Gear                []GearSlot           `json:"gear,omitempty"`
	Stats               []StatValue          `json:"stats,omitempty"`
	Professions         []ProfessionProgress `json:"professions,omitempty"`
	EquippedWing        *WingInfo            `json:"equippedWing,omitempty"`
	EquippedTitles      []TitleRef           `json:"equippedTitles,omitempty"`
	TotalSkillsCount    int                  `json:"totalSkillsCount,omitempty"`
	EquippedSkillsCount int                  `json:"equippedSkillsCount,omitempty"`
	DaevanionTotalNodes int                  `json:"daevanionTotalNodes,omitempty"`
	OwnedPetsCount      int                  `json:"ownedPetsCount,omitempty"`
	OwnedTitlesCount    int                  `json:"ownedTitlesCount,omitempty"`
	OwnedWingsCount     int                  `json:"ownedWingsCount,omitempty"`
	OwnedSkinsCount     int                  `json:"ownedSkinsCount,omitempty"`
	AbyssSeason         *AbyssSeasonStats    `json:"abyssSeason,omitempty"`
	Skills              []CharacterSkill     `json:"skills,omitempty"`
	Daevanion           []DaevanionBoard     `json:"daevanion,omitempty"`
	Genus               []GenusBranch        `json:"genus,omitempty"`
	Monoliths           []MonolithEntry      `json:"monoliths,omitempty"`
	Pantheon            []PantheonEntry      `json:"pantheon,omitempty"`
	Pets                []PetEntry           `json:"pets,omitempty"`
}

// CurrencyBalance is one currency entry from currencies.json.
type CurrencyBalance struct {
	ItemID   ItemID `json:"itemId"`
	ItemName string `json:"itemName"`
	Count    int64  `json:"count"`
}

// TicketEntry represents dungeon/abyss/nightmare entry counts & timers from tickets.json.
type TicketEntry struct {
	TicketID           int    `json:"ticketId"`
	Name               string `json:"name,omitempty"`
	Count              int    `json:"count"`
	Extra              int    `json:"extra,omitempty"`
	TimeMinutes        int    `json:"timeMinutes,omitempty"`
	TimeFormatted      string `json:"timeFormatted,omitempty"`
	ExtraTimeMinutes   int    `json:"extraTimeMinutes,omitempty"`
	ExtraTimeFormatted string `json:"extraTimeFormatted,omitempty"`
	NearCap            bool   `json:"nearCap,omitempty"`
	CapNote            string `json:"capNote,omitempty"`
}

// DropItem is one looted item count inside a farming stretch.
type DropItem struct {
	ItemID   ItemID `json:"itemId"`
	ItemName string `json:"itemName,omitempty"`
	Count    int64  `json:"count"`
}

// DropStretch is a farming interval on one map from Questlog's drops/*.json.
type DropStretch struct {
	DateID  string     `json:"dateId"`
	From    time.Time  `json:"from"`
	To      time.Time  `json:"to"`
	MapID   MapID      `json:"mapId"`
	MapName string     `json:"mapName,omitempty"`
	Items   []DropItem `json:"items"`
}
