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
