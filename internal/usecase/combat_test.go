package usecase

import (
	"testing"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

func TestSoulLabel(t *testing.T) {
	resolver := NewResolver(nil, nil, nil, "ru")
	bosses := map[int64]domain.EntityEntry{
		5001: {ID: 5001, Name: "Кромери"},
	}
	npcs := map[int64]domain.EntityEntry{
		200: {ID: 200, Name: "Лесной тролль"},
	}

	// 1. From bosses
	if got := resolver.SoulLabel(bosses, npcs, 5001); got != "Душа: Кромери" {
		t.Errorf("got %q, want 'Душа: Кромери'", got)
	}

	// 2. From npcs
	if got := resolver.SoulLabel(bosses, npcs, 200); got != "Душа: Лесной тролль" {
		t.Errorf("got %q, want 'Душа: Лесной тролль'", got)
	}

	// 3. From knownSoulNames
	if got := resolver.SoulLabel(bosses, npcs, 1025); got != "Душа горного тролля" {
		t.Errorf("got %q, want 'Душа горного тролля'", got)
	}

	// 4. Unknown fallback
	if got := resolver.SoulLabel(bosses, npcs, 9999); got != "Душа монстра #9999" {
		t.Errorf("got %q, want 'Душа монстра #9999'", got)
	}
}
