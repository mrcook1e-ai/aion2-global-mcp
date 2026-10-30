package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

// knownSystemTickets maps internal system ticket and timer IDs to human-readable names.
var knownSystemTickets = map[int]string{
	1:   "Дневной лимит подземелий",
	2:   "Еженедельный лимит подземелий",
	3:   "Арена (PvP)",
	4:   "Поле боя Бездны",
	5:   "Святилище испытаний",
	9:   "Рейдовая экспедиция",
	10:  "Осада крепости",
	11:  "Башня испытаний",
	12:  "Лабиринт измерений",
	101: "Мировой босс",
	102: "Полевой босс",
	201: "Охотничьи угодья (Тир 1)",
	202: "Охотничьи угодья (Тир 2)",
	203: "Охотничьи угодья (Тир 3)",
	204: "Охотничьи угодья (Тир 4)",
	205: "Охотничьи угодья (Тир 5)",
	206: "Охотничьи угодья (Тир 6)",
}

// knownMonolithNames maps regional monolith IDs to localized region names.
var knownMonolithNames = map[int]string{
	2001: "Монолит Альтгарда",
	2002: "Монолит Морхейма",
	2003: "Монолит Белуслана",
	2004: "Монолит Фоэты",
	2005: "Монолит Бертрона",
	2006: "Монолит Элтенена",
	2007: "Монолит Интердики",
	2008: "Монолит Келькмароса",
	2009: "Монолит Ингисона",
	2010: "Монолит Бездны",
	2011: "Монолит Аркадии",
	2012: "Монолит Пангеи",
}

// knownPantheonNames maps Pantheon deco/relic IDs to localized relic names.
var knownPantheonNames = map[int]string{
	1:  "Реликвия Неджакана",
	2:  "Реликвия Джикела",
	3:  "Реликвия Байзела",
	4:  "Реликвия Триниэль",
	5:  "Реликвия Кайсинеля",
	6:  "Реликвия Люмиэль",
	7:  "Реликвия Юстиэля",
	8:  "Реликвия Маркутана",
	9:  "Реликвия Эрискаль",
	10: "Реликвия Ариэль",
	11: "Реликвия Азарота",
	12: "Реликвия Сиэль",
	13: "Реликвия Исрафеля",
	14: "Реликвия Фрегиона",
	15: "Реликвия Месламтаэды",
}

func resolveTicketName(dungeons, nightmares map[int64]domain.EntityEntry, tid int) string {
	if name, ok := knownSystemTickets[tid]; ok {
		return name
	}
	if tid >= 60000001 && tid <= 60000099 {
		return fmt.Sprintf("Разлом кошмара (Спец. %d)", tid-60000000)
	}
	if tid >= 70000001 && tid <= 70000099 {
		return fmt.Sprintf("Сезонная активность #%d", tid-70000000)
	}
	if tid >= 100000 {
		baseID := int64(tid / 100)
		tier := tid % 100
		if tier > 0 && tier <= 30 {
			if d, ok := dungeons[baseID]; ok && d.Name != "" {
				return fmt.Sprintf("%s (T%d)", d.Name, tier)
			}
			if n, ok := nightmares[baseID]; ok && n.Name != "" {
				return fmt.Sprintf("%s (T%d)", n.Name, tier)
			}
		}
	}
	if d, ok := dungeons[int64(tid)]; ok && d.Name != "" {
		return d.Name
	}
	if n, ok := nightmares[int64(tid)]; ok && n.Name != "" {
		return n.Name
	}
	return ""
}

type CharacterOverview struct {
	Character           domain.Character         `json:"character"`
	Currencies          []domain.CurrencyBalance `json:"currencies,omitempty"`
	Tickets             []domain.TicketEntry     `json:"tickets,omitempty"`
	NearCapTicketsCount int                      `json:"nearCapTicketsCount,omitempty"`
	BagItemsUnique      int                      `json:"bagItemsUnique"`
	StorageUnique       int                      `json:"storageItemsUnique"`
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
	// Collect wing and title IDs across all characters for batch resolution
	var wingIDs, titleIDs []int64
	for _, ch := range chars {
		if ch.EquippedWing != nil && ch.EquippedWing.ID > 0 {
			wingIDs = append(wingIDs, ch.EquippedWing.ID)
		}
		for _, t := range ch.EquippedTitles {
			titleIDs = append(titleIDs, t.ID)
		}
	}
	wingEnts := s.resolver.ResolveEntities(ctx, domain.EntityWing, wingIDs, false)
	titleEnts := s.resolver.ResolveEntities(ctx, domain.EntityTitle, titleIDs, false)

	for i := range chars {
		profs, _ := s.profRepo.GetProfessions(ctx, chars[i].Key)
		chars[i].Professions = profs

		// Resolve wing name
		if chars[i].EquippedWing != nil && chars[i].EquippedWing.ID > 0 {
			if w, ok := wingEnts[chars[i].EquippedWing.ID]; ok {
				chars[i].EquippedWing.Name = w.Name
			}
		}
		// Resolve title names
		for ti := range chars[i].EquippedTitles {
			if te, ok := titleEnts[chars[i].EquippedTitles[ti].ID]; ok {
				chars[i].EquippedTitles[ti].Name = te.Name
			}
		}

		chars[i].Gear = nil
		chars[i].Stats = nil
		chars[i].Skills = nil
		chars[i].Daevanion = nil
		chars[i].Genus = nil
		chars[i].Monoliths = nil
		chars[i].Pantheon = nil
		chars[i].Pets = nil
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

func (s *CharacterService) GetCharacterOverview(ctx context.Context, nameOrKey string, includeCollections bool) (*CharacterOverview, error) {
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

	// Keep only publicly defined UI stats when the stat dictionary is loaded (filtering out internal packet modifiers)
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

	// Resolve Genus rolled stat names
	for bi := range ch.Genus {
		for ei := range ch.Genus[bi].Effects {
			ef := &ch.Genus[bi].Effects[ei]
			if ent, ok := statEntities[int64(ef.StatID)]; ok && ent.Name != "" {
				ef.StatName = ent.Name
			} else {
				ef.StatName = fmt.Sprintf("stat_%d", ef.StatID)
			}
		}
	}

	// Filter skills (equipped by default, all when includeCollections=true) & resolve localized names + tripods
	filteredSkills := make([]domain.CharacterSkill, 0, len(ch.Skills))
	var equippedSkillIDs, otherSkillIDs []int64
	for _, sk := range ch.Skills {
		if !includeCollections && !sk.Equipped {
			continue
		}
		filteredSkills = append(filteredSkills, sk)
		if sk.Equipped || len(sk.Specializations) > 0 {
			equippedSkillIDs = append(equippedSkillIDs, int64(sk.SkillID))
		} else {
			otherSkillIDs = append(otherSkillIDs, int64(sk.SkillID))
		}
	}
	skillEnts := s.resolver.ResolveEntities(ctx, domain.EntitySkill, equippedSkillIDs, true)
	if len(otherSkillIDs) > 0 {
		otherEnts := s.resolver.ResolveEntities(ctx, domain.EntitySkill, otherSkillIDs, false)
		for k, v := range otherEnts {
			if _, exists := skillEnts[k]; !exists {
				skillEnts[k] = v
			}
		}
	}
	for i := range filteredSkills {
		sid := int64(filteredSkills[i].SkillID)
		filteredSkills[i].Name = s.resolver.SkillLabel(skillEnts, sid)
		if ent, ok := skillEnts[sid]; ok {
			filteredSkills[i].SpecializationDetails = SkillSpecDescriptions(ent, filteredSkills[i].Specializations)
		}
	}
	sort.Slice(filteredSkills, func(i, j int) bool {
		if filteredSkills[i].Equipped != filteredSkills[j].Equipped {
			return filteredSkills[i].Equipped
		}
		return filteredSkills[i].Level > filteredSkills[j].Level
	})
	ch.Skills = filteredSkills

	// Resolve Wing & Equipped Titles
	if ch.EquippedWing != nil && ch.EquippedWing.ID > 0 {
		wings := s.resolver.ResolveEntities(ctx, domain.EntityWing, []int64{ch.EquippedWing.ID}, false)
		if w, ok := wings[ch.EquippedWing.ID]; ok {
			ch.EquippedWing.Name = w.Name
		}
	}
	if len(ch.EquippedTitles) > 0 {
		tIDs := make([]int64, len(ch.EquippedTitles))
		for i, t := range ch.EquippedTitles {
			tIDs[i] = t.ID
		}
		titleEnts := s.resolver.ResolveEntities(ctx, domain.EntityTitle, tIDs, false)
		for i := range ch.EquippedTitles {
			if te, ok := titleEnts[ch.EquippedTitles[i].ID]; ok {
				ch.EquippedTitles[i].Name = te.Name
			}
		}
	}

	// Resolve Monolith names
	for i := range ch.Monoliths {
		mid := ch.Monoliths[i].MonolithID
		if name, ok := knownMonolithNames[mid]; ok {
			ch.Monoliths[i].Name = name
		} else {
			ch.Monoliths[i].Name = fmt.Sprintf("Монолит #%d", mid)
		}
	}

	// Resolve Pantheon relics names
	for i := range ch.Pantheon {
		did := ch.Pantheon[i].DecoID
		if name, ok := knownPantheonNames[did]; ok {
			ch.Pantheon[i].Name = name
		} else {
			ch.Pantheon[i].Name = fmt.Sprintf("Реликвия #%d", did)
		}
	}

	// Daevanion & Pets compactness control
	if !includeCollections {
		for i := range ch.Daevanion {
			ch.Daevanion[i].Nodes = nil
		}
		ch.Pets = nil
	} else if len(ch.Pets) > 0 {
		petIDs := make([]int64, len(ch.Pets))
		for i, pt := range ch.Pets {
			petIDs[i] = pt.ID
		}
		petEnts := s.resolver.ResolveEntities(ctx, domain.EntityPet, petIDs, false)
		for i := range ch.Pets {
			if pe, ok := petEnts[ch.Pets[i].ID]; ok {
				ch.Pets[i].Name = pe.Name
			}
		}
		sort.Slice(ch.Pets, func(i, j int) bool {
			if ch.Pets[i].Level != ch.Pets[j].Level {
				return ch.Pets[i].Level > ch.Pets[j].Level
			}
			return ch.Pets[i].ID < ch.Pets[j].ID
		})
	}

	for i := range currencies {
		currencies[i].ItemName = s.resolver.ItemLabel(itemsByID, currencies[i].ItemID)
	}

	nearCapCount := 0
	var unknownDungeonIDs []int64
	for i := range tickets {
		if tickets[i].NearCap {
			nearCapCount++
		}
		tid := tickets[i].TicketID
		name := resolveTicketName(dungeons, nightmares, tid)
		if name != "" {
			tickets[i].Name = name
		} else if tid >= 1000 {
			unknownDungeonIDs = append(unknownDungeonIDs, int64(tid))
		} else {
			tickets[i].Name = fmt.Sprintf("Билет #%d", tid)
		}
	}
	if len(unknownDungeonIDs) > 0 {
		resolvedDungeons := s.resolver.ResolveEntities(ctx, domain.EntityDungeon, unknownDungeonIDs, false)
		for i := range tickets {
			if tickets[i].Name == "" {
				if ent, ok := resolvedDungeons[int64(tickets[i].TicketID)]; ok && ent.Name != "" {
					tickets[i].Name = ent.Name
				} else {
					tickets[i].Name = fmt.Sprintf("Билет #%d", tickets[i].TicketID)
				}
			}
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
		Character:           *ch,
		Currencies:          currencies,
		Tickets:             tickets,
		NearCapTicketsCount: nearCapCount,
		BagItemsUnique:      bagUnique,
		StorageUnique:       storageUnique,
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
			return items[i].Grade > items[j].Grade
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
