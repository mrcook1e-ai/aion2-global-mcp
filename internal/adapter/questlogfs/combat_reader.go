package questlogfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

func safeIDFilename(id string) (string, error) {
	clean := strings.TrimSpace(strings.TrimSuffix(id, ".json"))
	if clean == "" || clean == "index" || strings.ContainsAny(clean, `/\..`) {
		return "", fmt.Errorf("некорректный идентификатор записи: %q", id)
	}
	return clean + ".json", nil
}

func roundPct(frac float64) float64 {
	return math.Round(frac*1000) / 10
}

// flexCount accepts either a JSON number or an array (counted by length), since Questlog
// stores some counters (e.g. participant deaths) as a list of death timestamps in newer formats.
type flexCount int

func (f *flexCount) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if strings.HasPrefix(s, "[") {
		var arr []json.RawMessage
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*f = flexCount(len(arr))
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexCount(int(n))
	return nil
}

type rawSkillRow struct {
	Skill             int64   `json:"skill"`
	Damage            int64   `json:"damage"`
	Share             float64 `json:"share"`
	DPS               float64 `json:"dps"`
	Hits              int     `json:"hits"`
	Uses              int     `json:"uses"`
	CritRate          float64 `json:"critRate"`
	BackRate          float64 `json:"backRate"`
	PerfectRate       float64 `json:"perfectRate"`
	HardHitRate       float64 `json:"hardHitRate"`
	MultiHitRate      float64 `json:"multiHitRate"`
	Average           float64 `json:"average"`
	Max               int64   `json:"max"`
	AverageIntervalMs float64 `json:"averageIntervalMs"`
}

func convertSkills(rows []rawSkillRow, totalDamage int64, maxRows int) []domain.SkillBreakdown {
	if len(rows) == 0 {
		return nil
	}
	sorted := make([]rawSkillRow, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Damage > sorted[j].Damage })
	if maxRows > 0 && len(sorted) > maxRows {
		sorted = sorted[:maxRows]
	}

	out := make([]domain.SkillBreakdown, 0, len(sorted))
	for _, s := range sorted {
		if s.Skill <= 0 || s.Damage <= 0 {
			continue
		}
		share := s.Share
		if share == 0 && totalDamage > 0 {
			share = float64(s.Damage) / float64(totalDamage)
		}
		out = append(out, domain.SkillBreakdown{
			SkillID:           s.Skill,
			Damage:            s.Damage,
			SharePct:          roundPct(share),
			DPS:               math.Round(s.DPS*10) / 10,
			Hits:              s.Hits,
			Uses:              s.Uses,
			CritRatePct:       roundPct(s.CritRate),
			BackRatePct:       roundPct(s.BackRate),
			PerfectRatePct:    roundPct(s.PerfectRate),
			HardHitRatePct:    roundPct(s.HardHitRate),
			MultiHitRatePct:   roundPct(s.MultiHitRate),
			AverageHit:        int64(math.Round(s.Average)),
			MaxHit:            s.Max,
			AverageIntervalMs: int64(math.Round(s.AverageIntervalMs)),
		})
	}
	return out
}

// --- 1. Boss Encounters ---

func (r *Reader) ReadBossEncounters(ctx context.Context) ([]domain.BossEncounterSummary, error) {
	var raw struct {
		Entries []struct {
			ID              string  `json:"id"`
			StartedAt       int64   `json:"startedAt"`
			DurationSeconds float64 `json:"durationSeconds"`
			Damage          int64   `json:"damage"`
			DPS             float64 `json:"dps"`
			End             string  `json:"end"`
			NPC             int64   `json:"npc"`
			Killed          bool    `json:"killed"`
			Players         int     `json:"players"`
			Character       string  `json:"character"`
			SelfDPS         float64 `json:"selfDps"`
			PersonalBest    bool    `json:"personalBest"`
		} `json:"entries"`
	}
	err := readJSONSafe(ctx, filepath.Join(r.dataDir, "encounters", "index.json"), &raw)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]domain.BossEncounterSummary, 0, len(raw.Entries))
	for _, e := range raw.Entries {
		out = append(out, domain.BossEncounterSummary{
			ID:                e.ID,
			StartedAt:         time.UnixMilli(e.StartedAt).UTC(),
			DurationSeconds:   math.Round(e.DurationSeconds*100) / 100,
			DurationFormatted: domain.FormatSeconds(e.DurationSeconds),
			NpcID:             domain.NpcID(e.NPC),
			Character:         e.Character,
			Players:           e.Players,
			Killed:            e.Killed,
			EndReason:         e.End,
			TotalDamage:       e.Damage,
			PartyDPS:          int64(math.Round(e.DPS)),
			SelfDPS:           int64(math.Round(e.SelfDPS)),
			PersonalBest:      e.PersonalBest,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func (r *Reader) ReadBossEncounterDetail(ctx context.Context, encounterID string) (*domain.BossEncounterDetail, error) {
	fn, err := safeIDFilename(encounterID)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID           string `json:"id"`
		PersonalBest bool   `json:"personalBest"`
		Summary      struct {
			StartedAt       int64   `json:"startedAt"`
			DurationSeconds float64 `json:"durationSeconds"`
			Damage          int64   `json:"damage"`
			DPS             float64 `json:"dps"`
			End             string  `json:"end"`
			Server          int     `json:"server"`
			Latency         int     `json:"latency"`
			NPC             int64   `json:"npc"`
			Boss            *struct {
				NPC int64 `json:"npc"`
			} `json:"boss"`
			Participants []struct {
				Name        string        `json:"name"`
				Server      int           `json:"server"`
				Class       string        `json:"class"`
				Level       int           `json:"level"`
				GearScore   int           `json:"gearScore"`
				CombatPower int           `json:"combatPower"`
				Self        bool          `json:"self"`
				Damage      int64         `json:"damage"`
				DPS         float64       `json:"dps"`
				RDPS        float64       `json:"rdps"`
				Share       float64       `json:"share"`
				Healing     int64         `json:"healing"`
				HPS         float64       `json:"hps"`
				DamageTaken int64         `json:"damageTaken"`
				Deaths      flexCount     `json:"deaths"`
				Hits        int           `json:"hits"`
				CritRate    float64       `json:"critRate"`
				BackRate    float64       `json:"backRate"`
				PerfectRate float64       `json:"perfectRate"`
				HardHitRate float64       `json:"hardHitRate"`
				MultiHit    float64       `json:"multiHitRate"`
				HighestHit  int64         `json:"highestHit"`
				Skills      []rawSkillRow `json:"skills"`
			} `json:"participants"`
		} `json:"summary"`
	}
	if err := readJSONSafe(ctx, filepath.Join(r.dataDir, "encounters", fn), &raw); err != nil {
		return nil, err
	}

	npcID := raw.Summary.NPC
	if npcID <= 0 && raw.Summary.Boss != nil {
		npcID = raw.Summary.Boss.NPC
	}
	if npcID <= 0 {
		if list, _ := r.ReadBossEncounters(ctx); len(list) > 0 {
			cleanID := strings.TrimSuffix(fn, ".json")
			for _, it := range list {
				if it.ID == cleanID {
					npcID = int64(it.NpcID)
					break
				}
			}
		}
	}

	det := &domain.BossEncounterDetail{
		ID:                raw.ID,
		StartedAt:         time.UnixMilli(raw.Summary.StartedAt).UTC(),
		DurationSeconds:   math.Round(raw.Summary.DurationSeconds*100) / 100,
		DurationFormatted: domain.FormatSeconds(raw.Summary.DurationSeconds),
		ServerID:          domain.ServerID(raw.Summary.Server),
		LatencyMs:         raw.Summary.Latency,
		NpcID:             domain.NpcID(npcID),
		EndReason:         raw.Summary.End,
		TotalDamage:       raw.Summary.Damage,
		PartyDPS:          int64(math.Round(raw.Summary.DPS)),
		PersonalBest:      raw.PersonalBest,
	}

	for _, p := range raw.Summary.Participants {
		maxSkills := 10
		if p.Self {
			maxSkills = 18
		}
		det.Participants = append(det.Participants, domain.EncounterParticipant{
			Name:           p.Name,
			ServerID:       domain.ServerID(p.Server),
			Class:          p.Class,
			Level:          p.Level,
			GearScore:      p.GearScore,
			CombatPower:    p.CombatPower,
			Self:           p.Self,
			Damage:         p.Damage,
			DPS:            int64(math.Round(p.DPS)),
			RDPS:           int64(math.Round(p.RDPS)),
			SharePct:       roundPct(p.Share),
			Healing:        p.Healing,
			HPS:            int64(math.Round(p.HPS)),
			DamageTaken:    p.DamageTaken,
			Deaths:         int(p.Deaths),
			Hits:           p.Hits,
			CritRatePct:    roundPct(p.CritRate),
			BackRatePct:    roundPct(p.BackRate),
			PerfectRatePct: roundPct(p.PerfectRate),
			HardHitRatePct: roundPct(p.HardHitRate),
			MultiHitPct:    roundPct(p.MultiHit),
			HighestHit:     p.HighestHit,
			TopSkills:      convertSkills(p.Skills, p.Damage, maxSkills),
		})
	}
	sort.Slice(det.Participants, func(i, j int) bool { return det.Participants[i].Damage > det.Participants[j].Damage })
	return det, nil
}

// --- 2. Dungeon Runs ---

func (r *Reader) ReadDungeonRuns(ctx context.Context) ([]domain.DungeonRunSummary, error) {
	var raw struct {
		Entries []struct {
			ID           string  `json:"id"`
			Dungeon      int64   `json:"dungeon"`
			Boss         int64   `json:"boss"`
			Character    string  `json:"character"`
			EnteredAt    int64   `json:"enteredAt"`
			ClearSeconds float64 `json:"clearSeconds"`
			LeftSeconds  float64 `json:"leftSeconds"`
			Items        int     `json:"items"`
			Players      int     `json:"players"`
			Encounters   int     `json:"encounters"`
			SelfRunDPS   float64 `json:"selfRunDps"`
		} `json:"entries"`
	}
	err := readJSONSafe(ctx, filepath.Join(r.dataDir, "runs", "index.json"), &raw)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]domain.DungeonRunSummary, 0, len(raw.Entries))
	for _, e := range raw.Entries {
		out = append(out, domain.DungeonRunSummary{
			ID:             e.ID,
			DungeonID:      e.Dungeon,
			BossID:         domain.NpcID(e.Boss),
			Character:      e.Character,
			EnteredAt:      time.UnixMilli(e.EnteredAt).UTC(),
			ClearSeconds:   math.Round(e.ClearSeconds*100) / 100,
			ClearFormatted: domain.FormatSeconds(e.ClearSeconds),
			LeftSeconds:    math.Round(e.LeftSeconds*100) / 100,
			Players:        e.Players,
			Encounters:     e.Encounters,
			SelfRunDPS:     int64(math.Round(e.SelfRunDPS)),
			LootedCount:    e.Items,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EnteredAt.After(out[j].EnteredAt) })
	return out, nil
}

func (r *Reader) ReadDungeonRunDetail(ctx context.Context, runID string) (*domain.DungeonRunDetail, error) {
	fn, err := safeIDFilename(runID)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID        string    `json:"id"`
		Dungeon   int64     `json:"dungeon"`
		Character string    `json:"character"`
		EnteredAt int64     `json:"enteredAt"`
		StartedAt int64     `json:"startedAt"`
		ClearedAt int64     `json:"clearedAt"`
		LimitMs   int64     `json:"limitMs"`
		Deaths    flexCount `json:"deaths"`
		BossKills []struct {
			NPC      int64 `json:"npc"`
			OffsetMs int64 `json:"offsetMs"`
		} `json:"bossKills"`
		Items []struct {
			Item  int64 `json:"item"`
			Count int64 `json:"count"`
		} `json:"items"`
		Used []struct {
			Item  int64 `json:"item"`
			Count int64 `json:"count"`
		} `json:"used"`
		Members []struct {
			Name        string        `json:"name"`
			Server      int           `json:"server"`
			Class       string        `json:"class"`
			GearScore   int           `json:"gearScore"`
			CombatPower int           `json:"combatPower"`
			Self        bool          `json:"self"`
			Damage      int64         `json:"damage"`
			Healing     int64         `json:"healing"`
			DamageTaken int64         `json:"damageTaken"`
			Deaths      flexCount     `json:"deaths"`
			CritRate    float64       `json:"critRate"`
			BackRate    float64       `json:"backRate"`
			PerfectRate float64       `json:"perfectRate"`
			RunDPS      float64       `json:"runDps"`
			RunRDPS     float64       `json:"runRdps"`
			RunHPS      float64       `json:"runHps"`
			Skills      []rawSkillRow `json:"skills"`
		} `json:"members"`
	}
	if err := readJSONSafe(ctx, filepath.Join(r.dataDir, "runs", fn), &raw); err != nil {
		return nil, err
	}

	var clearSec float64
	startBase := raw.StartedAt
	if startBase <= 0 {
		startBase = raw.EnteredAt
	}
	if raw.ClearedAt > startBase && startBase > 0 {
		clearSec = float64(raw.ClearedAt-startBase) / 1000.0
	}

	det := &domain.DungeonRunDetail{
		ID:             raw.ID,
		DungeonID:      raw.Dungeon,
		Character:      raw.Character,
		EnteredAt:      time.UnixMilli(raw.EnteredAt).UTC(),
		ClearSeconds:   math.Round(clearSec*100) / 100,
		ClearFormatted: domain.FormatSeconds(clearSec),
		TimeLimitSec:   int(raw.LimitMs / 1000),
		TotalDeaths:    int(raw.Deaths),
	}

	for _, bk := range raw.BossKills {
		sec := int(bk.OffsetMs / 1000)
		det.BossKills = append(det.BossKills, domain.DungeonBossSplit{
			NpcID:           domain.NpcID(bk.NPC),
			OffsetSeconds:   sec,
			OffsetFormatted: domain.FormatSeconds(float64(sec)),
		})
	}
	for _, it := range raw.Items {
		if it.Item > 0 && it.Count > 0 {
			det.LootedItems = append(det.LootedItems, domain.DropItem{ItemID: domain.ItemID(it.Item), Count: it.Count})
		}
	}
	for _, u := range raw.Used {
		if u.Item > 0 && u.Count > 0 {
			det.UsedItems = append(det.UsedItems, domain.DropItem{ItemID: domain.ItemID(u.Item), Count: u.Count})
		}
	}
	for _, m := range raw.Members {
		maxSk := 8
		if m.Self {
			maxSk = 14
		}
		det.Members = append(det.Members, domain.DungeonMemberSummary{
			Name:           m.Name,
			ServerID:       domain.ServerID(m.Server),
			Class:          m.Class,
			GearScore:      m.GearScore,
			CombatPower:    m.CombatPower,
			Self:           m.Self,
			Damage:         m.Damage,
			RunDPS:         int64(math.Round(m.RunDPS)),
			RunRDPS:        int64(math.Round(m.RunRDPS)),
			Healing:        m.Healing,
			RunHPS:         int64(math.Round(m.RunHPS)),
			DamageTaken:    m.DamageTaken,
			Deaths:         int(m.Deaths),
			CritRatePct:    roundPct(m.CritRate),
			BackRatePct:    roundPct(m.BackRate),
			PerfectRatePct: roundPct(m.PerfectRate),
			TopSkills:      convertSkills(m.Skills, m.Damage, maxSk),
		})
	}
	sort.Slice(det.Members, func(i, j int) bool { return det.Members[i].Damage > det.Members[j].Damage })
	return det, nil
}

// --- 3. PvP Logs ---

func (r *Reader) ReadPvPEncounters(ctx context.Context) ([]domain.PvPEncounterSummary, error) {
	var raw struct {
		Entries []struct {
			ID              string    `json:"id"`
			StartedAt       int64     `json:"startedAt"`
			DurationSeconds float64   `json:"durationSeconds"`
			Dealt           int64     `json:"dealt"`
			Taken           int64     `json:"taken"`
			Kills           flexCount `json:"kills"`
			Deaths          flexCount `json:"deaths"`
			Character       string    `json:"character"`
			Opponents       []string  `json:"opponents"`
		} `json:"entries"`
	}
	err := readJSONSafe(ctx, filepath.Join(r.dataDir, "pvp", "index.json"), &raw)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]domain.PvPEncounterSummary, 0, len(raw.Entries))
	for _, e := range raw.Entries {
		out = append(out, domain.PvPEncounterSummary{
			ID:                e.ID,
			StartedAt:         time.UnixMilli(e.StartedAt).UTC(),
			DurationSeconds:   math.Round(e.DurationSeconds*100) / 100,
			DurationFormatted: domain.FormatSeconds(e.DurationSeconds),
			Character:         e.Character,
			DamageDealt:       e.Dealt,
			DamageTaken:       e.Taken,
			Kills:             int(e.Kills),
			Deaths:            int(e.Deaths),
			Opponents:         e.Opponents,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func (r *Reader) ReadPvPEncounterDetail(ctx context.Context, pvpID string) (*domain.PvPEncounterDetail, error) {
	fn, err := safeIDFilename(pvpID)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID      string `json:"id"`
		Summary struct {
			StartedAt       int64     `json:"startedAt"`
			DurationSeconds float64   `json:"durationSeconds"`
			Dealt           int64     `json:"dealt"`
			Taken           int64     `json:"taken"`
			DPS             float64   `json:"dps"`
			Kills           flexCount `json:"kills"`
			Assists         flexCount `json:"assists"`
			Deaths          flexCount `json:"deaths"`
			Server          int       `json:"server"`
			Self            struct {
				Name string `json:"name"`
			} `json:"self"`
			Opponents []struct {
				Name           string    `json:"name"`
				Server         int       `json:"server"`
				Class          string    `json:"class"`
				Level          int       `json:"level"`
				GearScore      int       `json:"gearScore"`
				CombatPower    int       `json:"combatPower"`
				AbyssGrade     int       `json:"abyssGrade"`
				MaxHP          int64     `json:"maxHp"`
				EngagedSeconds float64   `json:"engagedSeconds"`
				Dealt          int64     `json:"dealt"`
				DPS            float64   `json:"dps"`
				Taken          int64     `json:"taken"`
				TakenDPS       float64   `json:"takenDps"`
				Kills          flexCount `json:"kills"`
				Deaths         flexCount `json:"deaths"`
				DealtHits      struct {
					Skills []rawSkillRow `json:"skills"`
				} `json:"dealtHits"`
				TakenHits struct {
					Skills []rawSkillRow `json:"skills"`
				} `json:"takenHits"`
			} `json:"opponents"`
		} `json:"summary"`
	}
	if err := readJSONSafe(ctx, filepath.Join(r.dataDir, "pvp", fn), &raw); err != nil {
		return nil, err
	}

	det := &domain.PvPEncounterDetail{
		ID:                raw.ID,
		StartedAt:         time.UnixMilli(raw.Summary.StartedAt).UTC(),
		DurationSeconds:   math.Round(raw.Summary.DurationSeconds*100) / 100,
		DurationFormatted: domain.FormatSeconds(raw.Summary.DurationSeconds),
		Character:         raw.Summary.Self.Name,
		ServerID:          domain.ServerID(raw.Summary.Server),
		DamageDealt:       raw.Summary.Dealt,
		DamageTaken:       raw.Summary.Taken,
		DPS:               int64(math.Round(raw.Summary.DPS)),
		Kills:             int(raw.Summary.Kills),
		Assists:           int(raw.Summary.Assists),
		Deaths:            int(raw.Summary.Deaths),
	}

	for _, op := range raw.Summary.Opponents {
		det.Opponents = append(det.Opponents, domain.PvPOpponentBreakdown{
			Name:           op.Name,
			ServerID:       domain.ServerID(op.Server),
			Class:          op.Class,
			Level:          op.Level,
			GearScore:      op.GearScore,
			CombatPower:    op.CombatPower,
			AbyssGrade:     op.AbyssGrade,
			MaxHP:          op.MaxHP,
			EngagedSeconds: math.Round(op.EngagedSeconds*100) / 100,
			DamageDealt:    op.Dealt,
			DealtDPS:       int64(math.Round(op.DPS)),
			DamageTaken:    op.Taken,
			TakenDPS:       int64(math.Round(op.TakenDPS)),
			Kills:          int(op.Kills),
			Deaths:         int(op.Deaths),
			DealtSkills:    convertSkills(op.DealtHits.Skills, op.Dealt, 10),
			TakenSkills:    convertSkills(op.TakenHits.Skills, op.Taken, 10),
		})
	}
	sort.Slice(det.Opponents, func(i, j int) bool {
		return (det.Opponents[i].DamageDealt + det.Opponents[i].DamageTaken) > (det.Opponents[j].DamageDealt + det.Opponents[j].DamageTaken)
	})
	return det, nil
}

// --- 4. Personal Bests & Live Session Map Tracker ---

func (r *Reader) ReadPersonalBests(ctx context.Context) ([]domain.PersonalBestEntry, error) {
	var raw map[string]struct {
		Character       string  `json:"character"`
		Server          int     `json:"server"`
		NPC             int64   `json:"npc"`
		DPS             float64 `json:"dps"`
		Damage          int64   `json:"damage"`
		DurationSeconds float64 `json:"durationSeconds"`
		Encounter       string  `json:"encounter"`
		At              int64   `json:"at"`
	}
	err := readJSONSafe(ctx, filepath.Join(r.dataDir, "personal-bests.json"), &raw)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]domain.PersonalBestEntry, 0, len(raw))
	for _, pb := range raw {
		charKey := fmt.Sprintf("%d:%s", pb.Server, pb.Character)
		out = append(out, domain.PersonalBestEntry{
			CharacterKey:      charKey,
			Character:         pb.Character,
			ServerID:          domain.ServerID(pb.Server),
			NpcID:             domain.NpcID(pb.NPC),
			DPS:               int64(math.Round(pb.DPS)),
			Damage:            pb.Damage,
			DurationSeconds:   math.Round(pb.DurationSeconds*100) / 100,
			DurationFormatted: domain.FormatSeconds(pb.DurationSeconds),
			EncounterID:       pb.Encounter,
			AchievedAt:        time.UnixMilli(pb.At).UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DPS > out[j].DPS })
	return out, nil
}

func (r *Reader) ReadSessionTracker(ctx context.Context) (*domain.SessionTrackerSnapshot, error) {
	filePath := filepath.Join(r.dataDir, "map-tracker.json")
	fi, err := os.Stat(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	r.sessionMu.RLock()
	if r.cachedSession != nil && fi.ModTime().Equal(r.cachedModTime) {
		snap := r.cachedSession
		r.sessionMu.RUnlock()
		return snap, nil
	}
	r.sessionMu.RUnlock()

	var raw struct {
		Since     int64 `json:"since"`
		Character struct {
			Name   string `json:"name"`
			Server int    `json:"server"`
			Class  string `json:"class"`
		} `json:"character"`
		Kills       map[string]int64 `json:"kills"`
		Gathered    map[string]int64 `json:"gathered"`
		Looted      map[string]int64 `json:"looted"`
		SoulsGained map[string]int64 `json:"soulsGained"`
	}
	if err := readJSONSafe(ctx, filePath, &raw); err != nil {
		return nil, err
	}

	snap := &domain.SessionTrackerSnapshot{
		CharacterName: raw.Character.Name,
		ServerID:      domain.ServerID(raw.Character.Server),
		Class:         raw.Character.Class,
		Kills:         make(map[domain.NpcID]int64, len(raw.Kills)),
		Gathered:      make(map[int64]int64, len(raw.Gathered)),
		Looted:        make(map[domain.ItemID]int64, len(raw.Looted)),
		SoulsGained:   make(map[domain.NpcID]int64, len(raw.SoulsGained)),
	}
	if raw.Since > 0 {
		snap.Since = time.UnixMilli(raw.Since).UTC()
	}

	for k, cnt := range raw.Kills {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil && id > 0 && cnt > 0 {
			snap.Kills[domain.NpcID(id)] = cnt
		}
	}
	for k, cnt := range raw.Gathered {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil && id > 0 && cnt > 0 {
			snap.Gathered[id] = cnt
		}
	}
	for k, cnt := range raw.Looted {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil && id > 0 && cnt > 0 {
			snap.Looted[domain.ItemID(id)] = cnt
		}
	}
	for k, cnt := range raw.SoulsGained {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil && id > 0 && cnt > 0 {
			snap.SoulsGained[domain.NpcID(id)] = cnt
		}
	}

	r.sessionMu.Lock()
	r.cachedSession = snap
	r.cachedModTime = fi.ModTime()
	r.sessionMu.Unlock()

	return snap, nil
}
