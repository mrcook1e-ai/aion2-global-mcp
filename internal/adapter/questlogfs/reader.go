package questlogfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type Reader struct {
	dataDir string
}

func DefaultDataDir() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Questlog", "aion-2")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Roaming", "Questlog", "aion-2")
}

func New(dataDir string) *Reader {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	return &Reader{dataDir: dataDir}
}

func (r *Reader) DataDir() string { return r.dataDir }

func (r *Reader) Available() bool {
	st, err := os.Stat(r.dataDir)
	return err == nil && st.IsDir()
}

// readJSONSafe retries up to 4 times with jittered short pauses to survive concurrent atomic/truncating writes by Questlog Desktop.
func readJSONSafe(ctx context.Context, path string, out any) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(35*(attempt+1)) * time.Millisecond):
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return err
			}
			lastErr = err
			continue
		}
		if len(data) == 0 {
			lastErr = fmt.Errorf("empty file during concurrent write: %s", path)
			continue
		}
		if err := json.Unmarshal(data, out); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("read %s after retries: %w", path, lastErr)
}

func (r *Reader) ReadActiveCharacterName(ctx context.Context) (string, error) {
	var raw struct {
		Name string `json:"name"`
	}
	err := readJSONSafe(ctx, filepath.Join(r.dataDir, "self.json"), &raw)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return raw.Name, err
}

func (r *Reader) ReadCharacters(ctx context.Context) ([]domain.Character, error) {
	activeName, _ := r.ReadActiveCharacterName(ctx)

	var fileData struct {
		Characters map[string]struct {
			Key         string `json:"key"`
			ID          any    `json:"id"`
			Name        string `json:"name"`
			Server      int    `json:"server"`
			Class       string `json:"class"`
			Race        any    `json:"race"`
			Level       int    `json:"level"`
			GearScore   int    `json:"gearScore"`
			CombatPower int    `json:"combatPower"`
			Legion      string `json:"legion"`
			UpdatedAt   int64  `json:"updatedAt"`
			Gear        []struct {
				Slot       int   `json:"slot"`
				Item       int64 `json:"item"`
				Enchant    int   `json:"enchant"`
				Exceed     int   `json:"exceed"`
				Surpass    int   `json:"surpass"`
				ItemLevel  int   `json:"itemLevel"`
				Manastones []struct {
					Stone int64 `json:"stone"`
					Stat  int   `json:"stat"`
					Grade int   `json:"grade"`
				} `json:"manastones"`
				Godstones []int64 `json:"godstones"`
			} `json:"gear"`
			Stats []struct {
				Stat  int     `json:"stat"`
				Value float64 `json:"value"`
			} `json:"stats"`
			Skills []struct {
				Skill           int   `json:"skill"`
				Level           int   `json:"level"`
				Equipped        bool  `json:"equipped"`
				Specializations []int `json:"specializations"`
			} `json:"skills"`
			Daevanion []struct {
				Board int   `json:"board"`
				Nodes []int `json:"nodes"`
			} `json:"daevanion"`
			AbyssSeason *struct {
				At         int64 `json:"at"`
				Grade      int   `json:"grade"`
				Points     int64 `json:"points"`
				Kills      int   `json:"kills"`
				Deaths     int   `json:"deaths"`
				Assists    int   `json:"assists"`
				RankChange int   `json:"rankChange"`
			} `json:"abyssSeason"`
			Genus []struct {
				Genus   string `json:"genus"`
				Level   int    `json:"level"`
				Effects []struct {
					Slot   int     `json:"slot"`
					Grade  int     `json:"grade"`
					Option int     `json:"option"`
					Value  float64 `json:"value"`
				} `json:"effects"`
			} `json:"genus"`
			Monoliths []struct {
				Monolith int `json:"monolith"`
				Tokens   int `json:"tokens"`
			} `json:"monoliths"`
			Pantheon []struct {
				Slot int `json:"slot"`
				Deco int `json:"deco"`
			} `json:"pantheon"`
			EquippedWing int64   `json:"equippedWing"`
			Wings        []any   `json:"wings"`
			Titles       []int64 `json:"titles"`
			OwnedTitles  []int64 `json:"ownedTitles"`
			Skins        []any   `json:"skins"`
			Pets         []struct {
				Pet   int64 `json:"pet"`
				Level int   `json:"level"`
			} `json:"pets"`
		} `json:"characters"`
	}

	err := readJSONSafe(ctx, filepath.Join(r.dataDir, "characters.json"), &fileData)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]domain.Character, 0, len(fileData.Characters))
	for mapKey, raw := range fileData.Characters {
		key := raw.Key
		if key == "" {
			key = mapKey
		}
		raceStr := domain.NormalizeRaceQualification(raw.Race)
		if raceStr == "" && raw.Race != nil {
			raceStr = fmt.Sprint(raw.Race)
		}
		ch := domain.Character{
			Key:         key,
			ID:          fmt.Sprint(raw.ID),
			Name:        raw.Name,
			ServerID:    domain.ServerID(raw.Server),
			Class:       raw.Class,
			Race:        raceStr,
			Level:       raw.Level,
			GearScore:   raw.GearScore,
			CombatPower: raw.CombatPower,
			Legion:      raw.Legion,
			IsActive:    activeName != "" && strings.EqualFold(raw.Name, activeName),
		}
		if raw.UpdatedAt > 0 {
			ch.UpdatedAt = time.UnixMilli(raw.UpdatedAt).UTC()
		}
		for _, g := range raw.Gear {
			if g.Item <= 0 {
				continue
			}
			ench := g.Enchant
			exc := max(g.Exceed, g.Surpass)
			// In Questlog characters.json, breakthrough levels above +15 are stored as enchant=16..20 with surpass=0
			if exc == 0 && ench > 15 {
				exc = ench - 15
				ench = 15
			}
			gs := domain.GearSlot{
				Slot:         g.Slot,
				ItemID:       domain.ItemID(g.Item),
				ItemLevel:    g.ItemLevel,
				EnchantLevel: ench,
				ExceedLevel:  exc,
			}
			for _, m := range g.Manastones {
				if m.Stone > 0 {
					gs.ManastoneIDs = append(gs.ManastoneIDs, domain.ItemID(m.Stone))
				}
			}
			ch.Gear = append(ch.Gear, gs)
		}
		for _, st := range raw.Stats {
			if st.Stat > 0 {
				ch.Stats = append(ch.Stats, domain.StatValue{StatID: domain.StatID(st.Stat), Value: st.Value})
			}
		}
		sort.Slice(ch.Stats, func(i, j int) bool { return ch.Stats[i].StatID < ch.Stats[j].StatID })

		for _, sk := range raw.Skills {
			if sk.Skill <= 0 {
				continue
			}
			ch.TotalSkillsCount++
			if sk.Equipped {
				ch.EquippedSkillsCount++
			}
			ch.Skills = append(ch.Skills, domain.CharacterSkill{
				SkillID:         domain.SkillID(sk.Skill),
				Level:           sk.Level,
				Equipped:        sk.Equipped,
				Specializations: sk.Specializations,
			})
		}

		for _, db := range raw.Daevanion {
			if db.Board <= 0 {
				continue
			}
			ch.DaevanionTotalNodes += len(db.Nodes)
			ch.Daevanion = append(ch.Daevanion, domain.DaevanionBoard{
				BoardID:       db.Board,
				UnlockedCount: len(db.Nodes),
				Nodes:         db.Nodes,
			})
		}

		if raw.AbyssSeason != nil && (raw.AbyssSeason.Grade > 0 || raw.AbyssSeason.Points > 0 || raw.AbyssSeason.Kills > 0) {
			ab := &domain.AbyssSeasonStats{
				Grade:      raw.AbyssSeason.Grade,
				Points:     raw.AbyssSeason.Points,
				Kills:      raw.AbyssSeason.Kills,
				Deaths:     raw.AbyssSeason.Deaths,
				Assists:    raw.AbyssSeason.Assists,
				RankChange: raw.AbyssSeason.RankChange,
			}
			if raw.AbyssSeason.At > 0 {
				ab.UpdatedAt = time.UnixMilli(raw.AbyssSeason.At).UTC()
			}
			ch.AbyssSeason = ab
		}

		for _, gn := range raw.Genus {
			if gn.Genus == "" {
				continue
			}
			br := domain.GenusBranch{
				Name:  gn.Genus,
				Level: gn.Level,
			}
			for _, ef := range gn.Effects {
				br.Effects = append(br.Effects, domain.GenusEffect{
					Slot:   ef.Slot,
					Grade:  ef.Grade,
					StatID: domain.StatID(ef.Option),
					Value:  ef.Value,
				})
			}
			ch.Genus = append(ch.Genus, br)
		}

		for _, mn := range raw.Monoliths {
			if mn.Monolith > 0 {
				ch.Monoliths = append(ch.Monoliths, domain.MonolithEntry{
					MonolithID: mn.Monolith,
					Tokens:     mn.Tokens,
				})
			}
		}

		for _, pn := range raw.Pantheon {
			if pn.Deco > 0 {
				ch.Pantheon = append(ch.Pantheon, domain.PantheonEntry{
					Slot:   pn.Slot,
					DecoID: pn.Deco,
				})
			}
		}

		if raw.EquippedWing > 0 {
			ch.EquippedWing = &domain.WingInfo{ID: raw.EquippedWing}
		}
		for _, tid := range raw.Titles {
			if tid > 0 {
				ch.EquippedTitles = append(ch.EquippedTitles, domain.TitleRef{ID: tid})
			}
		}
		ch.OwnedPetsCount = len(raw.Pets)
		ch.OwnedTitlesCount = len(raw.OwnedTitles)
		ch.OwnedWingsCount = len(raw.Wings)
		ch.OwnedSkinsCount = len(raw.Skins)

		for _, pt := range raw.Pets {
			if pt.Pet > 0 {
				ch.Pets = append(ch.Pets, domain.PetEntry{ID: pt.Pet, Level: pt.Level})
			}
		}

		out = append(out, ch)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsActive != out[j].IsActive {
			return out[i].IsActive
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (r *Reader) ReadCurrencies(ctx context.Context) (map[string][]domain.CurrencyBalance, error) {
	var raw struct {
		Characters map[string][]struct {
			Item  int64 `json:"item"`
			Count int64 `json:"count"`
		} `json:"characters"`
	}
	err := readJSONSafe(ctx, filepath.Join(r.dataDir, "currencies.json"), &raw)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string][]domain.CurrencyBalance, len(raw.Characters))
	for charKey, list := range raw.Characters {
		for _, c := range list {
			out[charKey] = append(out[charKey], domain.CurrencyBalance{
				ItemID: domain.ItemID(c.Item),
				Count:  c.Count,
			})
		}
	}
	return out, nil
}

func (r *Reader) ReadTickets(ctx context.Context) (map[string][]domain.TicketEntry, error) {
	var raw struct {
		Characters map[string]struct {
			Tickets []struct {
				Ticket    int   `json:"ticket"`
				Count     int   `json:"count"`
				Extra     int   `json:"extra"`
				Time      int64 `json:"time"`
				ExtraTime int64 `json:"extraTime"`
			} `json:"tickets"`
		} `json:"characters"`
	}
	err := readJSONSafe(ctx, filepath.Join(r.dataDir, "tickets.json"), &raw)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string][]domain.TicketEntry, len(raw.Characters))
	for charKey, data := range raw.Characters {
		for _, t := range data.Tickets {
			if t.Ticket <= 0 || (t.Count == 0 && t.Extra == 0 && t.Time == 0 && t.ExtraTime == 0) {
				continue
			}
			timeMin := int(t.Time / 60000)
			extraMin := int(t.ExtraTime / 60000)
			var nearCap bool
			var capNote string
			if timeMin >= 380 {
				nearCap = true
				capNote = "таймер накоплен почти до потолка 7ч"
			} else if timeMin == 0 && t.Count >= 12 {
				nearCap = true
				capNote = "высокий запас билетов (риск упора в кап)"
			}
			out[charKey] = append(out[charKey], domain.TicketEntry{
				TicketID:           t.Ticket,
				Count:              t.Count,
				Extra:              t.Extra,
				TimeMinutes:        timeMin,
				TimeFormatted:      domain.FormatMinutes(timeMin),
				ExtraTimeMinutes:   extraMin,
				ExtraTimeFormatted: domain.FormatMinutes(extraMin),
				NearCap:            nearCap,
				CapNote:            capNote,
			})
		}
	}
	return out, nil
}

func (r *Reader) ReadDrops(ctx context.Context, limitDays int) ([]domain.DropStretch, error) {
	dropsDir := filepath.Join(r.dataDir, "drops")
	allFiles, err := filepath.Glob(filepath.Join(dropsDir, "*.json"))
	if err != nil || len(allFiles) == 0 {
		return nil, err
	}

	// Strictly keep daily files matching YYYY-MM-DD.json (never drops/index.json!)
	var dailyFiles []string
	for _, fp := range allFiles {
		base := filepath.Base(fp)
		if len(base) != 15 || !strings.HasSuffix(base, ".json") {
			continue
		}
		datePart := strings.TrimSuffix(base, ".json")
		if _, err := time.Parse("2006-01-02", datePart); err == nil {
			dailyFiles = append(dailyFiles, fp)
		}
	}
	if len(dailyFiles) == 0 {
		return nil, nil
	}

	sort.Strings(dailyFiles)
	if limitDays > 0 && len(dailyFiles) > limitDays {
		dailyFiles = dailyFiles[len(dailyFiles)-limitDays:]
	}

	const maxGapMs = 10 * 60 * 1000 // 10 minutes idle gap splits farming stretches

	var stretches []domain.DropStretch
	for _, fp := range dailyFiles {
		var raw struct {
			ID    string `json:"id"`
			Drops []struct {
				At    int64 `json:"at"`
				Map   int   `json:"map"`
				Item  int64 `json:"item"`
				Count int64 `json:"count"`
			} `json:"drops"`
		}
		if err := readJSONSafe(ctx, fp, &raw); err != nil || len(raw.Drops) == 0 {
			continue
		}

		sort.Slice(raw.Drops, func(i, j int) bool { return raw.Drops[i].At < raw.Drops[j].At })

		type activeStretch struct {
			fromMs int64
			toMs   int64
			counts map[domain.ItemID]int64
		}
		openByMap := make(map[domain.MapID]*activeStretch)

		flushStretch := func(mapID domain.MapID, st *activeStretch) {
			ds := domain.DropStretch{
				DateID: raw.ID,
				From:   time.UnixMilli(st.fromMs).UTC(),
				To:     time.UnixMilli(st.toMs).UTC(),
				MapID:  mapID,
			}
			for iid, cnt := range st.counts {
				ds.Items = append(ds.Items, domain.DropItem{ItemID: iid, Count: cnt})
			}
			stretches = append(stretches, ds)
		}

		for _, d := range raw.Drops {
			if d.Item <= 0 || d.Count <= 0 {
				continue
			}
			mapID := domain.MapID(d.Map)
			cur, ok := openByMap[mapID]
			if !ok {
				cur = &activeStretch{fromMs: d.At, toMs: d.At, counts: make(map[domain.ItemID]int64)}
				openByMap[mapID] = cur
			} else if d.At-cur.toMs > maxGapMs {
				flushStretch(mapID, cur)
				cur = &activeStretch{fromMs: d.At, toMs: d.At, counts: make(map[domain.ItemID]int64)}
				openByMap[mapID] = cur
			}
			if d.At > cur.toMs {
				cur.toMs = d.At
			}
			cur.counts[domain.ItemID(d.Item)] += d.Count
		}

		for mapID, st := range openByMap {
			flushStretch(mapID, st)
		}
	}
	return stretches, nil
}

// ReadLocalEntities extracts localized names from Questlog Desktop's local npc-display.json & game-data.json.
func (r *Reader) ReadLocalEntities(ctx context.Context, locale string) ([]domain.EntityEntry, error) {
	if locale == "" {
		locale = "ru"
	}
	var out []domain.EntityEntry
	now := time.Now().UTC()

	// 1. npc-display.json
	var npcFile struct {
		Value map[string]any `json:"value"`
	}
	if err := readJSONSafe(ctx, filepath.Join(r.dataDir, "npc-display.json"), &npcFile); err == nil {
		for k, v := range npcFile.Value {
			id, err := strconv.ParseInt(k, 10, 64)
			if err != nil || id <= 0 {
				continue
			}
			name := extractName(v, locale)
			if name != "" {
				out = append(out, domain.EntityEntry{
					Kind:      domain.EntityNPC,
					ID:        id,
					Locale:    locale,
					Name:      name,
					UpdatedAt: now,
				})
			}
		}
	}

	// 2. game-data.json (lists or maps of bosses, dungeons, nightmares, buffEffects, stats)
	var gdFile struct {
		Value map[string]json.RawMessage `json:"value"`
	}
	if err := readJSONSafe(ctx, filepath.Join(r.dataDir, "game-data.json"), &gdFile); err == nil {
		parseSection := func(raw json.RawMessage, kind domain.EntityKind, idField string) {
			if len(raw) == 0 {
				return
			}
			var arr []map[string]any
			if json.Unmarshal(raw, &arr) == nil {
				for _, row := range arr {
					id := extractAnyInt64(row[idField])
					if id <= 0 {
						id = extractAnyInt64(row["id"])
					}
					if id <= 0 {
						continue
					}
					if name := extractName(row, locale); name != "" {
						out = append(out, domain.EntityEntry{
							Kind:      kind,
							ID:        id,
							Locale:    locale,
							Name:      name,
							UpdatedAt: now,
						})
					}
				}
				return
			}
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil {
				for k, v := range m {
					id, err := strconv.ParseInt(k, 10, 64)
					if err != nil || id <= 0 {
						continue
					}
					if name := extractName(v, locale); name != "" {
						out = append(out, domain.EntityEntry{
							Kind:      kind,
							ID:        id,
							Locale:    locale,
							Name:      name,
							UpdatedAt: now,
						})
					}
				}
			}
		}

		parseSection(gdFile.Value["bosses"], domain.EntityBoss, "npc")
		parseSection(gdFile.Value["dungeons"], domain.EntityDungeon, "id")
		parseSection(gdFile.Value["nightmares"], domain.EntityNightmare, "id")
		parseSection(gdFile.Value["buffEffects"], domain.EntityBuff, "id")
		parseSection(gdFile.Value["stats"], domain.EntityStat, "statId")
	}

	return out, nil
}

func extractAnyInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}

func extractName(v any, locale string) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		if s, ok := x[locale].(string); ok && s != "" {
			return s
		}
		if s, ok := x["name"].(string); ok && s != "" {
			return s
		}
		if names, ok := x["name"].(map[string]any); ok {
			if s, ok := names[locale].(string); ok && s != "" {
				return s
			}
			if s, ok := names["en"].(string); ok && s != "" {
				return s
			}
		}
		if s, ok := x["en"].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
