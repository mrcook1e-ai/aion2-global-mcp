package questlogfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/sqlite"
	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

func TestSyncNDJSONInventory_BagAndStorageSeparation(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	store, err := sqlite.Open(ctx, filepath.Join(tmp, "test.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()

	qlDir := filepath.Join(tmp, "aion-2")
	logsDir := filepath.Join(qlDir, "combat-logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	logFile := filepath.Join(logsDir, "session1.ndjson")
	initialLines := `{"format":1,"selfName":"Cook1e"}
{"type":"inventory","at":1792535737000,"stacks":[{"instance":101,"item":610620016,"count":20},{"instance":102,"item":610610016,"count":5}]}
{"type":"player","at":1792535737100,"self":true,"name":"Cook1e","server":2319}
{"type":"inventory","at":1792535745000,"stacks":[{"instance":901,"item":610620016,"count":100},{"instance":902,"item":610530010,"count":4}]}
{"type":"inventory-update","at":1792535750000,"stacks":[{"instance":102,"item":610610016,"count":0,"reason":"use"}]}
`
	if err := os.WriteFile(logFile, []byte(initialLines), 0o644); err != nil {
		t.Fatal(err)
	}

	reader := New(qlDir)
	known := []domain.Character{{Key: "2319:Cook1e", Name: "Cook1e", ServerID: 2319}}

	files, evs, err := reader.SyncNDJSONInventory(ctx, store, known)
	if err != nil {
		t.Fatalf("SyncNDJSONInventory: %v", err)
	}
	if files != 1 || evs != 3 {
		t.Fatalf("got files=%d evs=%d, want 1 and 3", files, evs)
	}

	agg, err := store.GetAggregatedInventory(ctx, "2319:Cook1e")
	if err != nil {
		t.Fatal(err)
	}

	// Item 610620016 must have BOTH 20 in bag AND 100 in storage = 120 total!
	ore := agg[610620016]
	if ore.BagCount != 20 || ore.StorageCount != 100 || ore.TotalCount != 120 {
		t.Fatalf("ore counts mismatch: %+v (want bag=20 storage=100 total=120)", ore)
	}

	// Item 610610016 (instance 102) was consumed to count=0 -> should be deleted!
	if _, exists := agg[610610016]; exists {
		t.Fatalf("consumed item 610610016 should have been removed")
	}

	// Incremental tail test: append one more update line and ensure only the delta is processed!
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"inventory-update","at":1792535760000,"stacks":[{"instance":101,"item":610620016,"count":25}]}` + "\n")
	_ = f.Close()

	files2, evs2, err := reader.SyncNDJSONInventory(ctx, store, known)
	if err != nil {
		t.Fatal(err)
	}
	if files2 != 1 || evs2 != 1 {
		t.Fatalf("incremental sync: files=%d evs=%d, want 1 and 1", files2, evs2)
	}

	agg2, _ := store.GetAggregatedInventory(ctx, "2319:Cook1e")
	if agg2[610620016].TotalCount != 125 {
		t.Fatalf("updated ore total = %d, want 125", agg2[610620016].TotalCount)
	}
}

func TestReadDropsAndCharacter_IgnoresIndexAndSplitsExceed(t *testing.T) {
	ctx := context.Background()
	qlDir := filepath.Join(t.TempDir(), "aion-2")
	dropsDir := filepath.Join(qlDir, "drops")
	if err := os.MkdirAll(dropsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// index.json must be ignored even though alphabetically 'index.json' > '2026-10-26.json'
	_ = os.WriteFile(filepath.Join(dropsDir, "index.json"), []byte(`{"entries":[{"id":"old","stretches":[]}]}`), 0o644)
	_ = os.WriteFile(filepath.Join(dropsDir, "2026-10-26.json"), []byte(`{"id":"2026-10-26","drops":[{"at":1792963000000,"map":510032,"item":930100031,"count":50000},{"at":1792963120000,"map":510032,"item":610530001,"count":3}]}`), 0o644)

	_ = os.WriteFile(filepath.Join(qlDir, "characters.json"), []byte(`{
		"characters": {
			"2319:Cook1e": {
				"key": "2319:Cook1e", "name": "Cook1e", "server": 2319, "class": "ranger", "level": 45,
				"gear": [{"slot": 1, "item": 110430050, "enchant": 17, "surpass": 0, "itemLevel": 110}],
				"stats": [{"stat": 1, "value": 56}]
			}
		}
	}`), 0o644)

	_ = os.WriteFile(filepath.Join(qlDir, "tickets.json"), []byte(`{
		"characters": {
			"2319:Cook1e": {
				"tickets": [
					{"ticket": 1, "count": 7, "extra": 3, "time": 0, "extraTime": 0},
					{"ticket": 6, "count": 0, "extra": 0, "time": 0, "extraTime": 0},
					{"ticket": 3, "count": 0, "extra": 0, "time": 12000000, "extraTime": 6000000}
				]
			}
		}
	}`), 0o644)

	r := New(qlDir)
	stretches, err := r.ReadDrops(ctx, 1)
	if err != nil || len(stretches) != 1 {
		t.Fatalf("ReadDrops: len=%d err=%v, want 1", len(stretches), err)
	}

	chars, err := r.ReadCharacters(ctx)
	if err != nil || len(chars) != 1 {
		t.Fatalf("ReadCharacters: %v", err)
	}
	if chars[0].Gear[0].EnchantLevel != 15 || chars[0].Gear[0].ExceedLevel != 2 {
		t.Fatalf("gear exceed split failed: %+v (want enchant=15 exceed=2)", chars[0].Gear[0])
	}

	tkts, err := r.ReadTickets(ctx)
	if err != nil || len(tkts["2319:Cook1e"]) != 2 {
		t.Fatalf("ReadTickets: got %+v err=%v, want 2 non-zero entries", tkts["2319:Cook1e"], err)
	}
	if tkts["2319:Cook1e"][1].TimeMinutes != 200 || tkts["2319:Cook1e"][1].ExtraTimeMinutes != 100 {
		t.Fatalf("ticket timers mismatch: %+v", tkts["2319:Cook1e"][1])
	}
}
