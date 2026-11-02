package domain

// DPS formula constants and audit structures for AION 2 damage pipeline analysis.
// Reference: Verified Aion 2 damage formula with <1.2% deviation across 8-41s boss encounters.

// DPSAuditResult is the complete output of the character DPS audit tool.
type DPSAuditResult struct {
	Character    string           `json:"character"`
	Class        string           `json:"class"`
	Level        int              `json:"level"`
	CombatPower  int              `json:"combatPower"`
	GearScore    int              `json:"gearScore"`
	Mode         string           `json:"mode"` // pve, pvp, abyss
	TargetDef    int              `json:"targetDef"`
	Pipeline     DPSPipeline      `json:"pipeline"`
	GearAudit    GearAudit        `json:"gearAudit"`
	SkillAudit   SkillAudit       `json:"skillAudit"`
	MiscAudit    MiscAudit        `json:"miscAudit"`
	StatWeights  []StatWeight     `json:"statWeights"`
	Bottlenecks  []Bottleneck     `json:"bottlenecks"`
	PersonalBest *PersonalBestDPS `json:"personalBest,omitempty"`
}

// PersonalBestDPS is a compact reference to the player's actual measured DPS.
type PersonalBestDPS struct {
	BossName string  `json:"bossName"`
	DPS      int64   `json:"dps"`
	Duration float64 `json:"durationSeconds"`
	TotalDmg int64   `json:"totalDamage"`
}

// DPSPipeline decomposes all intermediate values of the Aion 2 damage formula.
type DPSPipeline struct {
	// Phase 1: Attack Power chain
	EffectivePureAttack float64 `json:"effectivePureAttack"`
	WeaponDmgBoost      float64 `json:"weaponDmgBoostMult"`
	MultiHitMult        float64 `json:"multiHitMult"`
	PowerShardMult      float64 `json:"powerShardMult"`
	AttackBonus         float64 `json:"attackBonus"`
	AttackIncreaseMult  float64 `json:"attackIncreaseMult"`
	FlatSituational     float64 `json:"flatSituational"`
	TotalAttackPower    float64 `json:"totalAttackPower"`

	// Phase 2: Defense mitigation
	Penetration float64 `json:"penetration"`
	Mitigation  float64 `json:"mitigation"`

	// Phase 3: Multiplicative buckets
	DmgBoostBucket     float64 `json:"dmgBoostBucket"`
	CritChance         float64 `json:"critChancePct"`
	CritDmgMult        float64 `json:"critDmgMult"`
	DoubleChance       float64 `json:"doubleChancePct"`
	DoubleExpectedMult float64 `json:"doubleExpectedMult"`

	// Speed & CDR
	CombatSpeedMult float64 `json:"combatSpeedMult"`
	IllusionCDR     float64 `json:"illusionCDR"`
}

// GearAudit details equipment analysis per slot.
type GearAudit struct {
	TotalSlots   int            `json:"totalSlots"`
	AuditedSlots int            `json:"auditedSlots"`
	Issues       []GearIssue    `json:"issues,omitempty"`
	SlotDetails  []GearSlotInfo `json:"slotDetails,omitempty"`
}

// GearSlotInfo describes one gear slot for the audit.
type GearSlotInfo struct {
	Slot         int      `json:"slot"`
	ItemName     string   `json:"itemName"`
	Grade        int      `json:"grade"`
	ItemLevel    int      `json:"itemLevel"`
	EnchantLevel int      `json:"enchantLevel"`
	ExceedLevel  int      `json:"exceedLevel"`
	Manastones   []string `json:"manastones,omitempty"`
	Issues       []string `json:"issues,omitempty"`
}

// GearIssue flags one concrete problem with a gear slot.
type GearIssue struct {
	Slot     int    `json:"slot"`
	ItemName string `json:"itemName"`
	Severity string `json:"severity"` // critical, warning, info
	Issue    string `json:"issue"`
	Fix      string `json:"fix"`
}

// SkillAudit covers Arcane Skills + Stigmas + Daevanion.
type SkillAudit struct {
	EquippedCount  int              `json:"equippedCount"`
	TotalCount     int              `json:"totalCount"`
	MaxSkillLevel  int              `json:"maxSkillLevel"`
	AvgSkillLevel  float64          `json:"avgSkillLevel"`
	Skills         []SkillAuditItem `json:"skills,omitempty"`
	DaevanionAudit DaevanionAudit   `json:"daevanionAudit"`
	Issues         []SkillIssue     `json:"issues,omitempty"`
}

// SkillAuditItem details one equipped skill.
type SkillAuditItem struct {
	SkillID         SkillID  `json:"skillId"`
	Name            string   `json:"name"`
	Level           int      `json:"level"`
	MaxLevel        int      `json:"maxLevel,omitempty"`
	Specializations []string `json:"specializations,omitempty"`
	Issues          []string `json:"issues,omitempty"`
}

// DaevanionAudit summarizes Daevanion board progress.
type DaevanionAudit struct {
	TotalNodes    int                     `json:"totalNodes"`
	BoardsSummary []DaevanionBoardSummary `json:"boards,omitempty"`
}

// DaevanionBoardSummary is per-board unlock status.
type DaevanionBoardSummary struct {
	BoardID       int `json:"boardId"`
	UnlockedCount int `json:"unlockedCount"`
}

// SkillIssue flags a problem with skill setup.
type SkillIssue struct {
	SkillName string `json:"skillName,omitempty"`
	Severity  string `json:"severity"` // critical, warning, info
	Issue     string `json:"issue"`
	Fix       string `json:"fix"`
}

// MiscAudit covers wings, pets, pet boards, Pantheon, titles, Genus, Monoliths.
type MiscAudit struct {
	Wing      *MiscEntry      `json:"wing,omitempty"`
	Titles    []MiscEntry     `json:"titles,omitempty"`
	Genus     []GenusAudit    `json:"genus,omitempty"`
	Monoliths []MonolithAudit `json:"monoliths,omitempty"`
	Pantheon  []PantheonAudit `json:"pantheon,omitempty"`
	Pets      *PetAudit       `json:"pets,omitempty"`
	Issues    []MiscIssue     `json:"issues,omitempty"`
}

// MiscEntry is a generic name+level pair for wings/titles.
type MiscEntry struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// GenusAudit shows one Genus branch rolled stats.
type GenusAudit struct {
	Branch  string        `json:"branch"`
	Level   int           `json:"level"`
	Effects []GenusEffect `json:"effects,omitempty"`
	Issues  []string      `json:"issues,omitempty"`
}

// MonolithAudit shows one monolith token progress.
type MonolithAudit struct {
	MonolithID int    `json:"monolithId"`
	Name       string `json:"name"`
	Tokens     int    `json:"tokens"`
}

// PantheonAudit shows one equipped Pantheon relic.
type PantheonAudit struct {
	Slot   int    `json:"slot"`
	DecoID int    `json:"decoId"`
	Name   string `json:"name"`
}

// PetAudit summarizes pet collection.
type PetAudit struct {
	OwnedCount  int    `json:"ownedCount"`
	TopPetName  string `json:"topPetName,omitempty"`
	TopPetLevel int    `json:"topPetLevel,omitempty"`
}

// MiscIssue flags a problem outside gear/skills.
type MiscIssue struct {
	Category string `json:"category"` // wing, pet, genus, monolith, pantheon, title
	Severity string `json:"severity"` // critical, warning, info
	Issue    string `json:"issue"`
	Fix      string `json:"fix"`
}

// StatWeight quantifies the marginal DPS value of incrementing one stat.
type StatWeight struct {
	Stat     string  `json:"stat"`
	DeltaDPS float64 `json:"deltaDps"`
	PctGain  float64 `json:"pctGain"`
	Bucket   string  `json:"bucket"`
}

// Bottleneck is a prioritized improvement recommendation.
type Bottleneck struct {
	Rank     int    `json:"rank"`
	Category string `json:"category"` // gear, skill, misc
	Severity string `json:"severity"` // critical, warning, info
	Issue    string `json:"issue"`
	Fix      string `json:"fix"`
	Impact   string `json:"impact,omitempty"`
}

// --- DPS Pipeline computation constants ---

const (
	// Default PvE target defense
	DefaultTargetDef          = 4500
	DefaultTargetCritResist   = 350
	DefaultTargetDoubleResist = 0.08

	// Perfect Chance formula
	PerfectPerRating = 0.00015
	PerfectCap       = 0.80

	// Multi-hit formula
	MultiHitDivisor     = 3300.0
	MultiHitAvgDmgBonus = 0.125

	// Crit formula
	CritBaseProb   = 0.08
	CritPer100Diff = 0.065
	CritCap        = 0.80
	CritFloor      = 0.05

	// Double formula
	DoubleDivisor = 2400.0
	DoubleCap     = 0.80

	// Defense coefficient (PvE)
	DefCoeffPvE = 0.1
	DefCoeffPvP = 0.01

	// Deity attribute scaling: +0.1% per point
	DeityAttackScale = 0.001

	// Rating to multiplier: 1000 = 1.0x
	RatingToMult = 1000.0

	// Enchant level thresholds
	MaxEnchantLevel  = 15
	MaxExceedLevel   = 5
	GoodEnchantLevel = 12

	// Gear grade thresholds (numeric)
	GradeHeroic    = 41
	GradeRelicGold = 51
)
