package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type CharacterOverview struct {
	Character      domain.Character         `json:"character"`
	Currencies     []domain.CurrencyBalance `json:"currencies,omitempty"`
	Tickets        []domain.TicketEntry     `json:"tickets,omitempty"`
	BagItemsUnique int                      `json:"bagItemsUnique"`
	StorageUnique  int                      `json:"storageItemsUnique"`
}

type CharacterService struct {
	localFS  domain.LocalTelemetryReader
	invRepo  domain.InventoryRepository
	profRepo domain.ProfessionRepository
	syncSvc  *SyncService
	resolver *Resolver
}

func NewCharacterService(
	localFS domain.LocalTelemetryReader,
	invRepo domain.InventoryRepository,
	profRepo domain.ProfessionRepository,
	syncSvc *SyncService,
	resolver *Resolver,
) *CharacterService {
	return &CharacterService{
		localFS:  localFS,
		invRepo:  invRepo,
		profRepo: profRepo,
		syncSvc:  syncSvc,
		resolver: resolver,
	}
}

func (s *CharacterService) ListCharacters(ctx context.Context) ([]domain.Character, error) {
	if s.localFS == nil || !s.localFS.Available() {
		return nil, fmt.Errorf("локальные данные Questlog недоступны (%s)", s.dataDirLabel())
	}
	chars, err := s.localFS.ReadCharacters(ctx)
	if err != nil {
		return nil, err
	}
	for i := range chars {
		profs, _ := s.profRepo.GetProfessions(ctx, chars[i].Key)
		chars[i].Professions = profs
		chars[i].Gear = nil
		chars[i].Stats = nil
	}
	return chars, nil
}

func (s *CharacterService) FindCharacter(ctx context.Context, nameOrKey string) (*domain.Character, error) {
	if s.localFS == nil || !s.localFS.Available() {
		return nil, fmt.Errorf("локальные данные Questlog недоступны (%s)", s.dataDirLabel())
	}
	chars, err := s.localFS.ReadCharacters(ctx)
	if err != nil {
		return nil, err
	}
	if len(chars) == 0 {
		return nil, fmt.Errorf("в characters.json пока нет сохранённых персонажей")
	}

	q := strings.TrimSpace(nameOrKey)
	if q == "" {
		return &chars[0], nil
	}

	var nameMatches []domain.Character
	for _, ch := range chars {
		if strings.EqualFold(ch.Key, q) {
			return &ch, nil
		}
		if strings.EqualFold(ch.Name, q) {
			nameMatches = append(nameMatches, ch)
		}
	}
	if len(nameMatches) == 1 {
		return &nameMatches[0], nil
	}
	if len(nameMatches) > 1 {
		keys := make([]string, len(nameMatches))
		for i, m := range nameMatches {
			keys[i] = m.Key
		}
		return nil, fmt.Errorf("найдено несколько персонажей с именем %q на разных серверах (%s) — укажи полный ключ serverId:Name", q, strings.Join(keys, ", "))
	}

	ql := strings.ToLower(q)
	for _, ch := range chars {
		if strings.Contains(strings.ToLower(ch.Name), ql) {
			return &ch, nil
		}
	}
	return nil, fmt.Errorf("персонаж %q не найден среди локальных персонажей Questlog", q)
}

func (s *CharacterService) GetCharacterOverview(ctx context.Context, nameOrKey string) (*CharacterOverview, error) {
	_, _ = s.syncSvc.SyncTelemetry(ctx)

	ch, err := s.FindCharacter(ctx, nameOrKey)
	if err != nil {
		return nil, err
	}

	profs, _ := s.profRepo.GetProfessions(ctx, ch.Key)
	ch.Professions = profs

	var itemIDs []domain.ItemID
	for _, g := range ch.Gear {
		itemIDs = append(itemIDs, g.ItemID)
		itemIDs = append(itemIDs, g.ManastoneIDs...)
	}

	curMap, _ := s.localFS.ReadCurrencies(ctx)
	currencies := curMap[ch.Key]
	for _, c := range currencies {
		itemIDs = append(itemIDs, c.ItemID)
	}

	tktMap, _ := s.localFS.ReadTickets(ctx)
	tickets := tktMap[ch.Key]

	itemsByID, _ := s.resolver.ResolveItems(ctx, itemIDs)
	statEntities := s.resolver.EntityMap(ctx, domain.EntityStat)
	dungeons := s.resolver.EntityMap(ctx, domain.EntityDungeon)
	nightmares := s.resolver.EntityMap(ctx, domain.EntityNightmare)

	for i := range ch.Gear {
		g := &ch.Gear[i]
		if it, ok := itemsByID[g.ItemID]; ok {
			g.ItemName = it.Name
			g.Grade = it.Grade
			if g.ItemLevel == 0 {
				g.ItemLevel = it.ItemLevel
			}
		} else {
			g.ItemName = s.resolver.ItemLabel(itemsByID, g.ItemID)
		}
		for _, mid := range g.ManastoneIDs {
			g.Manastones = append(g.Manastones, s.resolver.ItemLabel(itemsByID, mid))
		}
	}

	// Keep only publicly defined UI stats when the stat dictionary is loaded (filtering out internal packet modifiers like 206/551)
	if len(statEntities) > 0 {
		resolvedStats := make([]domain.StatValue, 0, len(ch.Stats))
		for _, st := range ch.Stats {
			if ent, ok := statEntities[int64(st.StatID)]; ok && ent.Name != "" {
				st.Name = ent.Name
				resolvedStats = append(resolvedStats, st)
			}
		}
		ch.Stats = resolvedStats
	} else {
		for i := range ch.Stats {
			ch.Stats[i].Name = fmt.Sprintf("stat_%d", ch.Stats[i].StatID)
		}
	}

	for i := range currencies {
		currencies[i].ItemName = s.resolver.ItemLabel(itemsByID, currencies[i].ItemID)
	}

	for i := range tickets {
		tid := tickets[i].TicketID
		// Dungeon ticket IDs encode dungeonId*100 + tier (e.g. 60000101 -> dungeon 600001 tier 1)
		if tid >= 10000000 {
			baseID := int64(tid / 100)
			tier := tid % 100
			if d, ok := dungeons[baseID]; ok && d.Name != "" {
				tickets[i].Name = fmt.Sprintf("%s (T%d)", d.Name, tier)
				continue
			}
			if n, ok := nightmares[baseID]; ok && n.Name != "" {
				tickets[i].Name = fmt.Sprintf("%s (T%d)", n.Name, tier)
				continue
			}
		}
		if d, ok := dungeons[int64(tid)]; ok && d.Name != "" {
			tickets[i].Name = d.Name
		}
	}

	inv, _ := s.invRepo.GetAggregatedInventory(ctx, ch.Key)
	var bagUnique, storageUnique int
	for _, a := range inv {
		if a.BagCount > 0 {
			bagUnique++
		}
		if a.StorageCount > 0 {
			storageUnique++
		}
	}

	return &CharacterOverview{
		Character:      *ch,
		Currencies:     currencies,
		Tickets:        tickets,
		BagItemsUnique: bagUnique,
		StorageUnique:  storageUnique,
	}, nil
}

func (s *CharacterService) GetInventory(ctx context.Context, nameOrKey, filterQuery, category string) (charKey string, items []domain.AggregatedItem, err error) {
	_, _ = s.syncSvc.SyncTelemetry(ctx)

	ch, err := s.FindCharacter(ctx, nameOrKey)
	if err != nil {
		return "", nil, err
	}

	aggMap, err := s.invRepo.GetAggregatedInventory(ctx, ch.Key)
	if err != nil {
		return ch.Key, nil, err
	}

	var unknownIDs []domain.ItemID
	for id, a := range aggMap {
		if a.ItemName == "" {
			unknownIDs = append(unknownIDs, id)
		}
	}
	if len(unknownIDs) > 0 {
		resolved, _ := s.resolver.ResolveItems(ctx, unknownIDs)
		for id, it := range resolved {
			if a, ok := aggMap[id]; ok {
				a.ItemName = it.Name
				a.Grade = it.Grade
				a.Category = it.MainCategory
				aggMap[id] = a
			}
		}
	}

	qLower := strings.ToLower(strings.TrimSpace(filterQuery))
	catLower := strings.ToLower(strings.TrimSpace(category))

	for _, a := range aggMap {
		if a.ItemName == "" {
			a.ItemName = fmt.Sprintf("#%d", a.ItemID)
		}
		if qLower != "" && !strings.Contains(strings.ToLower(a.ItemName), qLower) {
			continue
		}
		if catLower != "" && !strings.EqualFold(a.Category, catLower) {
			continue
		}
		items = append(items, a)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Grade != items[j].Grade {
			return items[i].Grade > items[i].Grade
		}
		return items[i].TotalCount > items[j].TotalCount
	})
	return ch.Key, items, nil
}

func (s *CharacterService) SetProfession(ctx context.Context, nameOrKey, rawProfession string, level int, grade string) (*domain.ProfessionProgress, error) {
	if level < 0 {
		return nil, fmt.Errorf("уровень ремесла не может быть отрицательным")
	}
	prof := domain.NormalizeProfession(rawProfession)
	if prof == "" {
		return nil, fmt.Errorf("укажи профессию: blacksmithing, jewelcrafting, tailoring, alchemy или cooking")
	}
	ch, err := s.FindCharacter(ctx, nameOrKey)
	if err != nil {
		return nil, err
	}
	canonicalGrade, internalLvl, _ := domain.NormalizeMastery(grade, level)
	p := domain.ProfessionProgress{
		CharacterKey: ch.Key,
		Profession:   prof,
		MasteryGrade: canonicalGrade,
		MasteryLevel: internalLvl,
	}
	if err := s.profRepo.UpsertProfession(ctx, p); err != nil {
		return nil, err
	}
	return s.profRepo.GetProfession(ctx, ch.Key, prof)
}

func (s *CharacterService) dataDirLabel() string {
	if s.localFS == nil {
		return "none"
	}
	return s.localFS.DataDir()
}
