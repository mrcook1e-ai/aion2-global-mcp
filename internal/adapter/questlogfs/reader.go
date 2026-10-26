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
			gs := domain.GearSlot{
				Slot:         g.Slot,
				ItemID:       domain.ItemID(g.Item),
				ItemLevel:    g.ItemLevel,
				EnchantLevel: g.Enchant,
				ExceedLevel:  max(g.Exceed, g.Surpass),
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
			Tickets map[string]struct {
				Current int `json:"current"`
				Max     int `json:"max"`
			} `json:"tickets"`
			Stacks []struct {
				Item  int64 `json:"item"`
				Count int   `json:"count"`
			} `json:"stacks"`
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
		for tid, t := range data.Tickets {
			out[charKey] = append(out[charKey], domain.TicketEntry{
				ID:      tid,
				Current: t.Current,
				Max:     t.Max,
			})
		}
		for _, st := range data.Stacks {
			if st.Item > 0 && st.Count > 0 {
				out[charKey] = append(out[charKey], domain.TicketEntry{
					ID:      "ticket_item_" + strconv.FormatInt(st.Item, 10),
					Current: st.Count,
					ItemID:  domain.ItemID(st.Item),
				})
			}
		}
	}
	return out, nil
}

func (r *Reader) ReadDrops(ctx context.Context, limitDays int) ([]domain.DropStretch, error) {
	dropsDir := filepath.Join(r.dataDir, "drops")
	files, err := filepath.Glob(filepath.Join(dropsDir, "*.json"))
	if err != nil || len(files) == 0 {
		return nil, err
	}
	sort.Strings(files)
	if limitDays > 0 && len(files) > limitDays {
		files = files[len(files)-limitDays:]
	}

	var stretches []domain.DropStretch
	for _, fp := range files {
		var raw struct {
			Entries []struct {
				ID        string `json:"id"`
				Stretches []struct {
					From  int64 `json:"from"`
					To    int64 `json:"to"`
					Map   int   `json:"map"`
					Items []struct {
						Item  int64 `json:"item"`
						Count int64 `json:"count"`
					} `json:"items"`
				} `json:"stretches"`
			} `json:"entries"`
		}
		if err := readJSONSafe(ctx, fp, &raw); err != nil {
			continue
		}
		for _, e := range raw.Entries {
			for _, st := range e.Stretches {
				ds := domain.DropStretch{
					DateID: e.ID,
					From:   time.UnixMilli(st.From).UTC(),
					To:     time.UnixMilli(st.To).UTC(),
					MapID:  domain.MapID(st.Map),
				}
				for _, it := range st.Items {
					if it.Item > 0 && it.Count > 0 {
						ds.Items = append(ds.Items, domain.DropItem{
							ItemID: domain.ItemID(it.Item),
							Count:  it.Count,
						})
					}
				}
				stretches = append(stretches, ds)
			}
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

	// 2. game-data.json (bosses, dungeons, nightmares, buffEffects, stats)
	var gdFile struct {
		Value struct {
			Bosses      map[string]any `json:"bosses"`
			Dungeons    map[string]any `json:"dungeons"`
			Nightmares  map[string]any `json:"nightmares"`
			BuffEffects map[string]any `json:"buffEffects"`
			Stats       map[string]any `json:"stats"`
		} `json:"value"`
	}
	if err := readJSONSafe(ctx, filepath.Join(r.dataDir, "game-data.json"), &gdFile); err == nil {
		appendMap := func(m map[string]any, kind domain.EntityKind) {
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
		appendMap(gdFile.Value.Bosses, domain.EntityBoss)
		appendMap(gdFile.Value.Dungeons, domain.EntityDungeon)
		appendMap(gdFile.Value.Nightmares, domain.EntityNightmare)
		appendMap(gdFile.Value.BuffEffects, domain.EntityBuff)
		appendMap(gdFile.Value.Stats, domain.EntityStat)
	}

	return out, nil
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
