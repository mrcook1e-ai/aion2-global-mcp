package domain

import "time"

// SkillBreakdown summarizes damage/hit metrics of one skill in PvE or PvP logs.
type SkillBreakdown struct {
	SkillID           int64   `json:"skillId"`
	SkillName         string  `json:"skillName,omitempty"`
	Damage            int64   `json:"damage"`
	SharePct          float64 `json:"sharePct"`
	DPS               float64 `json:"dps,omitempty"`
	Hits              int     `json:"hits"`
	Uses              int     `json:"uses,omitempty"`
	CritRatePct       float64 `json:"critRatePct,omitempty"`
	BackRatePct       float64 `json:"backRatePct,omitempty"`
	PerfectRatePct    float64 `json:"perfectRatePct,omitempty"`
	HardHitRatePct    float64 `json:"hardHitRatePct,omitempty"`
	MultiHitRatePct   float64 `json:"multiHitRatePct,omitempty"`
	AverageHit        int64   `json:"averageHit,omitempty"`
	MaxHit            int64   `json:"maxHit,omitempty"`
	AverageIntervalMs int64   `json:"averageIntervalMs,omitempty"`
}

// BossEncounterSummary is one entry from encounters/index.json.
type BossEncounterSummary struct {
	ID                string    `json:"id"`
	StartedAt         time.Time `json:"startedAt"`
	DurationSeconds   float64   `json:"durationSeconds"`
	DurationFormatted string    `json:"durationFormatted"`
	NpcID             NpcID     `json:"npcId"`
	BossName          string    `json:"bossName,omitempty"`
	Character         string    `json:"character"`
	Players           int       `json:"players"`
	Killed            bool      `json:"killed"`
	EndReason         string    `json:"endReason"` // "kill", "wipe", "reset"
	TotalDamage       int64     `json:"totalDamage"`
	PartyDPS          int64     `json:"partyDps"`
	SelfDPS           int64     `json:"selfDps"`
	PersonalBest      bool      `json:"personalBest,omitempty"`
}

// EncounterParticipant details one party member's performance inside a boss encounter.
type EncounterParticipant struct {
	Name           string           `json:"name"`
	ServerID       ServerID         `json:"serverId,omitempty"`
	Class          string           `json:"class"`
	Level          int              `json:"level,omitempty"`
	GearScore      int              `json:"gearScore,omitempty"`
	CombatPower    int              `json:"combatPower,omitempty"`
	Self           bool             `json:"self,omitempty"`
	Damage         int64            `json:"damage"`
	DPS            int64            `json:"dps"`
	RDPS           int64            `json:"rdps,omitempty"`
	SharePct       float64          `json:"sharePct"`
	Healing        int64            `json:"healing,omitempty"`
	HPS            int64            `json:"hps,omitempty"`
	DamageTaken    int64            `json:"damageTaken,omitempty"`
	Deaths         int              `json:"deaths,omitempty"`
	Hits           int              `json:"hits"`
	CritRatePct    float64          `json:"critRatePct"`
	BackRatePct    float64          `json:"backRatePct"`
	PerfectRatePct float64          `json:"perfectRatePct"`
	HardHitRatePct float64          `json:"hardHitRatePct"`
	MultiHitPct    float64          `json:"multiHitPct"`
	HighestHit     int64            `json:"highestHit,omitempty"`
	TopSkills      []SkillBreakdown `json:"topSkills,omitempty"`
}

// BossEncounterDetail represents full rotation & party analytics from encounters/<uuid>.json.
type BossEncounterDetail struct {
	ID                string                 `json:"id"`
	StartedAt         time.Time              `json:"startedAt"`
	DurationSeconds   float64                `json:"durationSeconds"`
	DurationFormatted string                 `json:"durationFormatted"`
	ServerID          ServerID               `json:"serverId,omitempty"`
	LatencyMs         int                    `json:"latencyMs,omitempty"`
	NpcID             NpcID                  `json:"npcId,omitempty"`
	BossName          string                 `json:"bossName,omitempty"`
	EndReason         string                 `json:"endReason"`
	TotalDamage       int64                  `json:"totalDamage"`
	PartyDPS          int64                  `json:"partyDps"`
	PersonalBest      bool                   `json:"personalBest,omitempty"`
	Participants      []EncounterParticipant `json:"participants"`
}

// DungeonRunSummary is one entry from runs/index.json.
type DungeonRunSummary struct {
	ID             string    `json:"id"`
	DungeonID      int64     `json:"dungeonId"`
	DungeonName    string    `json:"dungeonName,omitempty"`
	BossID         NpcID     `json:"bossId,omitempty"`
	BossName       string    `json:"bossName,omitempty"`
	Character      string    `json:"character,omitempty"`
	EnteredAt      time.Time `json:"enteredAt"`
	ClearSeconds   float64   `json:"clearSeconds"`
	ClearFormatted string    `json:"clearFormatted"`
	LeftSeconds    float64   `json:"leftSeconds,omitempty"`
	Players        int       `json:"players"`
	Encounters     int       `json:"encounters"`
	SelfRunDPS     int64     `json:"selfRunDps"`
	LootedCount    int       `json:"lootedCount"`
}

// DungeonBossSplit records when a boss died relative to the run start.
type DungeonBossSplit struct {
	NpcID           NpcID  `json:"npcId"`
	BossName        string `json:"bossName,omitempty"`
	OffsetSeconds   int    `json:"offsetSeconds"`
	OffsetFormatted string `json:"offsetFormatted"`
}

// DungeonMemberSummary summarizes a party member's overall dungeon run contribution.
type DungeonMemberSummary struct {
	Name           string           `json:"name"`
	ServerID       ServerID         `json:"serverId,omitempty"`
	Class          string           `json:"class"`
	GearScore      int              `json:"gearScore,omitempty"`
	CombatPower    int              `json:"combatPower,omitempty"`
	Self           bool             `json:"self,omitempty"`
	Damage         int64            `json:"damage"`
	RunDPS         int64            `json:"runDps"`
	RunRDPS        int64            `json:"runRdps,omitempty"`
	Healing        int64            `json:"healing,omitempty"`
	RunHPS         int64            `json:"runHps,omitempty"`
	DamageTaken    int64            `json:"damageTaken,omitempty"`
	Deaths         int              `json:"deaths"`
	CritRatePct    float64          `json:"critRatePct"`
	BackRatePct    float64          `json:"backRatePct"`
	PerfectRatePct float64          `json:"perfectRatePct"`
	TopSkills      []SkillBreakdown `json:"topSkills,omitempty"`
}

// DungeonRunDetail represents full dungeon run telemetry from runs/<uuid>.json.
type DungeonRunDetail struct {
	ID             string                 `json:"id"`
	DungeonID      int64                  `json:"dungeonId"`
	DungeonName    string                 `json:"dungeonName,omitempty"`
	Character      string                 `json:"character,omitempty"`
	EnteredAt      time.Time              `json:"enteredAt"`
	ClearSeconds   float64                `json:"clearSeconds"`
	ClearFormatted string                 `json:"clearFormatted"`
	TimeLimitSec   int                    `json:"timeLimitSec,omitempty"`
	TotalDeaths    int                    `json:"totalDeaths"`
	BossKills      []DungeonBossSplit     `json:"bossKills,omitempty"`
	LootedItems    []DropItem             `json:"lootedItems,omitempty"`
	UsedItems      []DropItem             `json:"usedItems,omitempty"`
	Members        []DungeonMemberSummary `json:"members,omitempty"`
}

// PvPEncounterSummary is one skirmish entry from pvp/index.json.
type PvPEncounterSummary struct {
	ID                string    `json:"id"`
	StartedAt         time.Time `json:"startedAt"`
	DurationSeconds   float64   `json:"durationSeconds"`
	DurationFormatted string    `json:"durationFormatted"`
	Character         string    `json:"character"`
	DamageDealt       int64     `json:"damageDealt"`
	DamageTaken       int64     `json:"damageTaken"`
	Kills             int       `json:"kills"`
	Deaths            int       `json:"deaths"`
	Opponents         []string  `json:"opponents"`
}

// PvPOpponentBreakdown details combat against one specific enemy player in a PvP fight.
type PvPOpponentBreakdown struct {
	Name           string           `json:"name"`
	ServerID       ServerID         `json:"serverId,omitempty"`
	Class          string           `json:"class"`
	Level          int              `json:"level,omitempty"`
	GearScore      int              `json:"gearScore,omitempty"`
	CombatPower    int              `json:"combatPower,omitempty"`
	AbyssGrade     int              `json:"abyssGrade,omitempty"`
	MaxHP          int64            `json:"maxHp,omitempty"`
	EngagedSeconds float64          `json:"engagedSeconds"`
	DamageDealt    int64            `json:"damageDealt"` // damage WE dealt to this opponent
	DealtDPS       int64            `json:"dealtDps"`
	DamageTaken    int64            `json:"damageTaken"` // damage WE took from this opponent
	TakenDPS       int64            `json:"takenDps"`
	Kills          int              `json:"kills,omitempty"`
	Deaths         int              `json:"deaths,omitempty"`
	DealtSkills    []SkillBreakdown `json:"dealtSkills,omitempty"`
	TakenSkills    []SkillBreakdown `json:"takenSkills,omitempty"` // exact enemy skills that hit us!
}

// PvPEncounterDetail represents full incoming/outgoing skill breakdown from pvp/<uuid>.json.
type PvPEncounterDetail struct {
	ID                string                 `json:"id"`
	StartedAt         time.Time              `json:"startedAt"`
	DurationSeconds   float64                `json:"durationSeconds"`
	DurationFormatted string                 `json:"durationFormatted"`
	Character         string                 `json:"character"`
	ServerID          ServerID               `json:"serverId,omitempty"`
	DamageDealt       int64                  `json:"damageDealt"`
	DamageTaken       int64                  `json:"damageTaken"`
	DPS               int64                  `json:"dps"`
	Kills             int                    `json:"kills"`
	Assists           int                    `json:"assists"`
	Deaths            int                    `json:"deaths"`
	Opponents         []PvPOpponentBreakdown `json:"opponents"`
}

// PersonalBestEntry is one boss DPS record from personal-bests.json.
type PersonalBestEntry struct {
	CharacterKey      string    `json:"characterKey"`
	Character         string    `json:"character"`
	ServerID          ServerID  `json:"serverId"`
	NpcID             NpcID     `json:"npcId"`
	BossName          string    `json:"bossName,omitempty"`
	DPS               int64     `json:"dps"`
	Damage            int64     `json:"damage"`
	DurationSeconds   float64   `json:"durationSeconds"`
	DurationFormatted string    `json:"durationFormatted"`
	EncounterID       string    `json:"encounterId"`
	AchievedAt        time.Time `json:"achievedAt"`
}

// TrackedNpcCount summarizes kill/soul count for one NPC in map-tracker.json.
type TrackedNpcCount struct {
	NpcID NpcID  `json:"npcId"`
	Name  string `json:"name,omitempty"`
	Count int64  `json:"count"`
}

// SessionTrackerSnapshot represents the raw live session state from map-tracker.json.
type SessionTrackerSnapshot struct {
	CharacterName string
	ServerID      ServerID
	Class         string
	Since         time.Time
	Kills         map[NpcID]int64
	Gathered      map[int64]int64
	Looted        map[ItemID]int64
	SoulsGained   map[NpcID]int64
}
