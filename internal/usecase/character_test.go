package usecase

import (
	"testing"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

func TestResolveTicketName(t *testing.T) {
	dungeons := map[int64]domain.EntityEntry{
		1001: {ID: 1001, Name: "Храм Огня"},
	}
	nightmares := map[int64]domain.EntityEntry{
		2001: {ID: 2001, Name: "Кошмар Альтгарда"},
	}

	tests := []struct {
		ticketID int
		want     string
	}{
		{1, "Дневной лимит подземелий"},
		{3, "Арена (PvP)"},
		{4, "Поле боя Бездны"},
		{101, "Мировой босс"},
		{201, "Охотничьи угодья (Тир 1)"},
		{206, "Охотничьи угодья (Тир 6)"},
		{60000002, "Разлом кошмара (Спец. 2)"},
		{70000005, "Сезонная активность #5"},
		{100101, "Храм Огня (T1)"},
		{200102, "Кошмар Альтгарда (T2)"},
		{1001, "Храм Огня"},
		{2001, "Кошмар Альтгарда"},
	}

	for _, tc := range tests {
		got := resolveTicketName(dungeons, nightmares, tc.ticketID)
		if got != tc.want {
			t.Errorf("resolveTicketName(tid=%d) = %q; want %q", tc.ticketID, got, tc.want)
		}
	}
}

func TestKnownMonolithAndPantheonNames(t *testing.T) {
	if name, ok := knownMonolithNames[2001]; !ok || name != "Монолит Альтгарда" {
		t.Errorf("knownMonolithNames[2001] = %q, want 'Монолит Альтгарда'", name)
	}
	if name, ok := knownPantheonNames[10]; !ok || name != "Реликвия Ариэль" {
		t.Errorf("knownPantheonNames[10] = %q, want 'Реликвия Ариэль'", name)
	}
}
