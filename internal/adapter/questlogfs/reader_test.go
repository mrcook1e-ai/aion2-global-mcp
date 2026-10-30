package questlogfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

func TestReadDropsLootSessions(t *testing.T) {
	tempDir := t.TempDir()
	lootDir := filepath.Join(tempDir, "loot-sessions")
	if err := os.MkdirAll(lootDir, 0755); err != nil {
		t.Fatalf("mkdir loot-sessions: %v", err)
	}

	indexContent := `{
		"format": 1,
		"version": 1,
		"entries": [
			{
				"id": "b6c5163e-3332-477f-86b2-a1b907e49f78",
				"startedAt": 1793132754475,
				"activeMs": 42102729,
				"maps": [1110, 510032, 20],
				"items": [
					{"item": 930100031, "source": "drop", "server": 2319, "count": 1147629},
					{"item": 610530014, "source": "drop", "server": 2319, "count": 67},
					{"item": 534660207, "source": "quest", "server": 2319, "count": 10}
				]
			},
			{
				"id": "day-2026-10-26",
				"startedAt": 1792963054292,
				"activeMs": 3751172,
				"maps": [504001, 1110],
				"items": [
					{"item": 610530001, "source": "other", "count": 22}
				]
			}
		]
	}`

	if err := os.WriteFile(filepath.Join(lootDir, "index.json"), []byte(indexContent), 0644); err != nil {
		t.Fatalf("write index.json: %v", err)
	}

	reader := New(tempDir)
	stretches, err := reader.ReadDrops(context.Background(), 7)
	if err != nil {
		t.Fatalf("ReadDrops error: %v", err)
	}

	if len(stretches) != 2 {
		t.Fatalf("expected 2 stretches, got %d", len(stretches))
	}

	// Verify items filtering (quest source was excluded)
	var foundKinah, foundQuestItem bool
	for _, st := range stretches {
		for _, it := range st.Items {
			if it.ItemID == 930100031 {
				foundKinah = true
			}
			if it.ItemID == 534660207 {
				foundQuestItem = true
			}
		}
	}
	if !foundKinah {
		t.Errorf("expected kinah item 930100031 to be present")
	}
	if foundQuestItem {
		t.Errorf("expected quest item 534660207 to be excluded")
	}

	// Verify primary map detection (skips 1110)
	for _, st := range stretches {
		if st.DateID == "b6c5163e-3332-477f-86b2-a1b907e49f78" {
			if st.MapID != domain.MapID(510032) {
				t.Errorf("expected mapID 510032, got %d", st.MapID)
			}
			expectedDur := 42102729 * time.Millisecond
			actualDur := st.To.Sub(st.From)
			if actualDur != expectedDur {
				t.Errorf("expected duration %v, got %v", expectedDur, actualDur)
			}
		}
		if st.DateID == "day-2026-10-26" {
			if st.MapID != domain.MapID(504001) {
				t.Errorf("expected mapID 504001, got %d", st.MapID)
			}
		}
	}
}
