package usecase

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type BossEncounterQuery struct {
	EncounterID       string
	Character         string
	BossQuery         string
	NpcID             int64
	OnlyKills         bool
	OnlyPersonalBests bool
	Limit             int
}

type BossEncountersOut struct {
	Detail       *domain.BossEncounterDetail   `json:"detail,omitempty"`
	TotalMatched int                           `json:"totalMatched,omitempty"`
	Encounters   []domain.BossEncounterSummary `json:"encounters,omitempty"`
}

type DungeonRunsQuery struct {
	RunID        string
	Character    string
	DungeonQuery string
	DungeonID    int64
	Limit        int
}

type DungeonRunsOut struct {
	Detail       *domain.DungeonRunDetail   `json:"detail,omitempty"`
	TotalMatched int                        `json:"totalMatched,omitempty"`
	Runs         []domain.DungeonRunSummary `json:"runs,omitempty"`
}

type PvPLogsQuery struct {
	PvPID         string
	Character     string
	OpponentQuery string
	Limit         int
}

type PvPLogsOut struct {
	Detail       *domain.PvPEncounterDetail   `json:"detail,omitempty"`
	TotalMatched int                          `json:"totalMatched,omitempty"`
	TotalKills   int                          `json:"totalKills,omitempty"`
	TotalDeaths  int                          `json:"totalDeaths,omitempty"`
	Encounters   []domain.PvPEncounterSummary `json:"encounters,omitempty"`
}

type PersonalBestsOut struct {
	TotalMatched int                        `json:"totalMatched"`
	Records      []domain.PersonalBestEntry `json:"records"`
}

type SessionTrackerOut struct {
	Character       string                   `json:"character"`
	ServerID        domain.ServerID          `json:"serverId,omitempty"`
	Class           string                   `json:"class,omitempty"`
	Since           time.Time                `json:"since"`
	ActiveHours     float64                  `json:"activeHours"`
	TotalKills      int64                    `json:"totalKills"`
	KillsPerHour    int64                    `json:"killsPerHour,omitempty"`
	TotalGathered   int64                    `json:"totalGathered"`
	TotalSouls      int64                    `json:"totalSouls"`
	NetLootValueEst int64                    `json:"netLootValueEst"`
	TopKilledNPCs   []domain.TrackedNpcCount `json:"topKilledNpcs,omitempty"`
	TopSoulsNPCs    []domain.TrackedNpcCount `json:"topSoulsNpcs,omitempty"`
	TopLootedItems  []ValuedLootEntry        `json:"topLootedItems,omitempty"`
}

type CombatService struct {
	localFS  domain.LocalTelemetryReader
	charSvc  *CharacterService
	syncSvc  *SyncService
	resolver *Resolver
}

func NewCombatService(
	localFS domain.LocalTelemetryReader,
	charSvc *CharacterService,
	syncSvc *SyncService,
	resolver *Resolver,
) *CombatService {
	return &CombatService{
		localFS:  localFS,
		charSvc:  charSvc,
		syncSvc:  syncSvc,
		resolver: resolver,
	}
}

func (s *CombatService) ensureAvailable() error {
	if s.localFS == nil || !s.localFS.Available() {
		return fmt.Errorf("локальные боевые логи Questlog недоступны")
	}
	return nil
}

func matchCharFilter(charName, filter string) bool {
	f := strings.TrimSpace(filter)
	if f == "" {
		return true
	}
	if idx := strings.IndexByte(f, ':'); idx >= 0 {
		f = f[idx+1:]
	}
	return strings.EqualFold(charName, f) || strings.Contains(strings.ToLower(charName), strings.ToLower(f))
}

// --- 1. Boss Encounters ---

func (s *CombatService) GetBossEncounters(ctx context.Context, q BossEncounterQuery) (*BossEncountersOut, error) {
	if err := s.ensureAvailable(); err != nil {
		return nil, err
	}
	bosses := s.resolver.EntityMap(ctx, domain.EntityBoss)
	npcs := s.resolver.EntityMap(ctx, domain.EntityNPC)

	if strings.TrimSpace(q.EncounterID) != "" {
		det, err := s.localFS.ReadBossEncounterDetail(ctx, q.EncounterID)
		if err != nil {
			return nil, err
		}
		if det.NpcID > 0 {
			det.BossName = s.resolver.NpcOrBossLabel(bosses, npcs, det.NpcID)
		}
		var skillIDs []int64
		for _, p := range det.Participants {
			for _, sk := range p.TopSkills {
				skillIDs = append(skillIDs, sk.SkillID)
			}
		}
		skillEnts := s.resolver.ResolveEntities(ctx, domain.EntitySkill, skillIDs, false)
		for pi := range det.Participants {
			for si := range det.Participants[pi].TopSkills {
				sid := det.Participants[pi].TopSkills[si].SkillID
				det.Participants[pi].TopSkills[si].SkillName = s.resolver.SkillLabel(skillEnts, sid)
			}
		}
		return &BossEncountersOut{Detail: det}, nil
	}

	all, err := s.localFS.ReadBossEncounters(ctx)
	if err != nil {
		return nil, err
	}

	bqLower := strings.ToLower(strings.TrimSpace(q.BossQuery))
	var matched []domain.BossEncounterSummary
	for _, e := range all {
		e.BossName = s.resolver.NpcOrBossLabel(bosses, npcs, e.NpcID)
		if !matchCharFilter(e.Character, q.Character) {
			continue
		}
		if q.NpcID > 0 && int64(e.NpcID) != q.NpcID {
			continue
		}
		if q.OnlyKills && !e.Killed {
			continue
		}
		if q.OnlyPersonalBests && !e.PersonalBest {
			continue
		}
		if bqLower != "" && !strings.Contains(strings.ToLower(e.BossName), bqLower) {
			continue
		}
		matched = append(matched, e)
	}

	total := len(matched)
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if len(matched) > limit {
		matched = matched[:limit]
	}

	return &BossEncountersOut{
		TotalMatched: total,
		Encounters:   matched,
	}, nil
}

// --- 2. Dungeon Runs ---

func (s *CombatService) GetDungeonRuns(ctx context.Context, q DungeonRunsQuery) (*DungeonRunsOut, error) {
	if err := s.ensureAvailable(); err != nil {
		return nil, err
	}
	dungeons := s.resolver.EntityMap(ctx, domain.EntityDungeon)
	nightmares := s.resolver.EntityMap(ctx, domain.EntityNightmare)
	bosses := s.resolver.EntityMap(ctx, domain.EntityBoss)
	npcs := s.resolver.EntityMap(ctx, domain.EntityNPC)

	resolveDungeonName := func(did int64) string {
		if d, ok := dungeons[did]; ok && d.Name != "" {
			return d.Name
		}
		if n, ok := nightmares[did]; ok && n.Name != "" {
			return n.Name
		}
		if did > 0 {
			res := s.resolver.ResolveEntities(ctx, domain.EntityDungeon, []int64{did}, false)
			if ent, ok := res[did]; ok && ent.Name != "" {
				return ent.Name
			}
		}
		return fmt.Sprintf("Dungeon #%d", did)
	}

	if strings.TrimSpace(q.RunID) != "" {
		det, err := s.localFS.ReadDungeonRunDetail(ctx, q.RunID)
		if err != nil {
			return nil, err
		}
		det.DungeonName = resolveDungeonName(det.DungeonID)
		for i := range det.BossKills {
			det.BossKills[i].BossName = s.resolver.NpcOrBossLabel(bosses, npcs, det.BossKills[i].NpcID)
		}

		var itemIDs []domain.ItemID
		for _, it := range det.LootedItems {
			itemIDs = append(itemIDs, it.ItemID)
		}
		for _, u := range det.UsedItems {
			itemIDs = append(itemIDs, u.ItemID)
		}
		itemsMap, _ := s.resolver.ResolveItems(ctx, itemIDs)
		for i := range det.LootedItems {
			det.LootedItems[i].ItemName = s.resolver.ItemLabel(itemsMap, det.LootedItems[i].ItemID)
		}
		for i := range det.UsedItems {
			det.UsedItems[i].ItemName = s.resolver.ItemLabel(itemsMap, det.UsedItems[i].ItemID)
		}

		var skillIDs []int64
		for _, m := range det.Members {
			for _, sk := range m.TopSkills {
				skillIDs = append(skillIDs, sk.SkillID)
			}
		}
		skillEnts := s.resolver.ResolveEntities(ctx, domain.EntitySkill, skillIDs, false)
		for mi := range det.Members {
			for si := range det.Members[mi].TopSkills {
				sid := det.Members[mi].TopSkills[si].SkillID
				det.Members[mi].TopSkills[si].SkillName = s.resolver.SkillLabel(skillEnts, sid)
			}
		}
		return &DungeonRunsOut{Detail: det}, nil
	}

	all, err := s.localFS.ReadDungeonRuns(ctx)
	if err != nil {
		return nil, err
	}

	dqLower := strings.ToLower(strings.TrimSpace(q.DungeonQuery))
	var matched []domain.DungeonRunSummary
	for _, r := range all {
		r.DungeonName = resolveDungeonName(r.DungeonID)
		if r.BossID > 0 {
			r.BossName = s.resolver.NpcOrBossLabel(bosses, npcs, r.BossID)
		}
		if q.Character != "" && r.Character != "" && !matchCharFilter(r.Character, q.Character) {
			continue
		}
		if q.DungeonID > 0 && r.DungeonID != q.DungeonID {
			continue
		}
		if dqLower != "" &&
			!strings.Contains(strings.ToLower(r.DungeonName), dqLower) &&
			!strings.Contains(strings.ToLower(r.BossName), dqLower) {
			continue
		}
		matched = append(matched, r)
	}

	total := len(matched)
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if len(matched) > limit {
		matched = matched[:limit]
	}

	return &DungeonRunsOut{
		TotalMatched: total,
		Runs:         matched,
	}, nil
}

// --- 3. PvP Logs ---

func (s *CombatService) GetPvPLogs(ctx context.Context, q PvPLogsQuery) (*PvPLogsOut, error) {
	if err := s.ensureAvailable(); err != nil {
		return nil, err
	}

	if strings.TrimSpace(q.PvPID) != "" {
		det, err := s.localFS.ReadPvPEncounterDetail(ctx, q.PvPID)
		if err != nil {
			return nil, err
		}
		var skillIDs []int64
		for _, op := range det.Opponents {
			for _, sk := range op.DealtSkills {
				skillIDs = append(skillIDs, sk.SkillID)
			}
			for _, sk := range op.TakenSkills {
				skillIDs = append(skillIDs, sk.SkillID)
			}
		}
		skillEnts := s.resolver.ResolveEntities(ctx, domain.EntitySkill, skillIDs, false)
		for oi := range det.Opponents {
			for si := range det.Opponents[oi].DealtSkills {
				sid := det.Opponents[oi].DealtSkills[si].SkillID
				det.Opponents[oi].DealtSkills[si].SkillName = s.resolver.SkillLabel(skillEnts, sid)
			}
			for si := range det.Opponents[oi].TakenSkills {
				sid := det.Opponents[oi].TakenSkills[si].SkillID
				det.Opponents[oi].TakenSkills[si].SkillName = s.resolver.SkillLabel(skillEnts, sid)
			}
		}
		return &PvPLogsOut{Detail: det}, nil
	}

	all, err := s.localFS.ReadPvPEncounters(ctx)
	if err != nil {
		return nil, err
	}

	oppLower := strings.ToLower(strings.TrimSpace(q.OpponentQuery))
	var matched []domain.PvPEncounterSummary
	totalKills, totalDeaths := 0, 0
	for _, e := range all {
		if !matchCharFilter(e.Character, q.Character) {
			continue
		}
		if oppLower != "" {
			foundOpp := false
			for _, op := range e.Opponents {
				if strings.Contains(strings.ToLower(op), oppLower) {
					foundOpp = true
					break
				}
			}
			if !foundOpp {
				continue
			}
		}
		totalKills += e.Kills
		totalDeaths += e.Deaths
		matched = append(matched, e)
	}

	total := len(matched)
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if len(matched) > limit {
		matched = matched[:limit]
	}

	return &PvPLogsOut{
		TotalMatched: total,
		TotalKills:   totalKills,
		TotalDeaths:  totalDeaths,
		Encounters:   matched,
	}, nil
}

// --- 4. Personal Bests & Live Session Tracker ---

func (s *CombatService) GetPersonalBests(ctx context.Context, character, bossQuery string, limit int) (*PersonalBestsOut, error) {
	if err := s.ensureAvailable(); err != nil {
		return nil, err
	}
	all, err := s.localFS.ReadPersonalBests(ctx)
	if err != nil {
		return nil, err
	}
	bosses := s.resolver.EntityMap(ctx, domain.EntityBoss)
	npcs := s.resolver.EntityMap(ctx, domain.EntityNPC)
	bqLower := strings.ToLower(strings.TrimSpace(bossQuery))

	var matched []domain.PersonalBestEntry
	for _, pb := range all {
		pb.BossName = s.resolver.NpcOrBossLabel(bosses, npcs, pb.NpcID)
		if character != "" && !strings.EqualFold(pb.CharacterKey, character) && !matchCharFilter(pb.Character, character) {
			continue
		}
		if bqLower != "" && !strings.Contains(strings.ToLower(pb.BossName), bqLower) {
			continue
		}
		matched = append(matched, pb)
	}

	total := len(matched)
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return &PersonalBestsOut{TotalMatched: total, Records: matched}, nil
}

func (s *CombatService) GetSessionTracker(ctx context.Context) (*SessionTrackerOut, error) {
	if err := s.ensureAvailable(); err != nil {
		return nil, err
	}
	_, _ = s.syncSvc.SyncTelemetry(ctx)

	snap, err := s.localFS.ReadSessionTracker(ctx)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return nil, fmt.Errorf("файл map-tracker.json пока отсутствует")
	}

	bosses := s.resolver.EntityMap(ctx, domain.EntityBoss)
	npcs := s.resolver.EntityMap(ctx, domain.EntityNPC)

	out := &SessionTrackerOut{
		Character: snap.CharacterName,
		ServerID:  snap.ServerID,
		Class:     snap.Class,
		Since:     snap.Since,
	}
	if !snap.Since.IsZero() {
		hrs := time.Since(snap.Since).Hours()
		if hrs > 0 {
			out.ActiveHours = math.Round(hrs*10) / 10
		}
	}

	for id, cnt := range snap.Kills {
		out.TotalKills += cnt
		out.TopKilledNPCs = append(out.TopKilledNPCs, domain.TrackedNpcCount{
			NpcID: id,
			Name:  s.resolver.NpcOrBossLabel(bosses, npcs, id),
			Count: cnt,
		})
	}
	sort.Slice(out.TopKilledNPCs, func(i, j int) bool { return out.TopKilledNPCs[i].Count > out.TopKilledNPCs[j].Count })
	if len(out.TopKilledNPCs) > 15 {
		out.TopKilledNPCs = out.TopKilledNPCs[:15]
	}
	if out.ActiveHours >= 0.1 {
		out.KillsPerHour = int64(math.Round(float64(out.TotalKills) / out.ActiveHours))
	}

	for _, cnt := range snap.Gathered {
		out.TotalGathered += cnt
	}

	for id, cnt := range snap.SoulsGained {
		out.TotalSouls += cnt
		out.TopSoulsNPCs = append(out.TopSoulsNPCs, domain.TrackedNpcCount{
			NpcID: id,
			Name:  s.resolver.NpcOrBossLabel(bosses, npcs, id),
			Count: cnt,
		})
	}
	sort.Slice(out.TopSoulsNPCs, func(i, j int) bool { return out.TopSoulsNPCs[i].Count > out.TopSoulsNPCs[j].Count })
	if len(out.TopSoulsNPCs) > 10 {
		out.TopSoulsNPCs = out.TopSoulsNPCs[:10]
	}

	var lootIDs []domain.ItemID
	for iid := range snap.Looted {
		lootIDs = append(lootIDs, iid)
	}
	itemsMap, _ := s.resolver.ResolveItems(ctx, lootIDs)
	var tradableIDs []domain.ItemID
	for _, iid := range lootIDs {
		if domain.IsKinahItem(iid) {
			continue
		}
		if it, ok := itemsMap[iid]; ok && it.Tradable != nil && *it.Tradable {
			tradableIDs = append(tradableIDs, iid)
		}
	}

	srvID := snap.ServerID
	if srvID <= 0 {
		if ch, _ := s.charSvc.FindCharacter(ctx, snap.CharacterName); ch != nil {
			srvID = ch.ServerID
		}
	}
	var quotes map[domain.ItemID]*domain.MarketQuote
	if srvID > 0 && len(tradableIDs) > 0 {
		quotes, _ = s.resolver.EnsureMarketQuotes(ctx, srvID, tradableIDs, false)
	}

	var directKinah, grossMarket int64
	for iid, cnt := range snap.Looted {
		if domain.IsKinahItem(iid) {
			directKinah += cnt
			out.TopLootedItems = append(out.TopLootedItems, ValuedLootEntry{
				ItemID:     iid,
				Name:       s.resolver.ItemLabel(itemsMap, iid),
				Count:      cnt,
				UnitPrice:  1,
				TotalValue: cnt,
			})
			continue
		}
		price, _, _ := quotes[iid].ConservativeSellPrice()
		if price <= 0 {
			continue
		}
		val := price * cnt
		grossMarket += val
		out.TopLootedItems = append(out.TopLootedItems, ValuedLootEntry{
			ItemID:     iid,
			Name:       s.resolver.ItemLabel(itemsMap, iid),
			Count:      cnt,
			UnitPrice:  price,
			TotalValue: val,
		})
	}
	sort.Slice(out.TopLootedItems, func(i, j int) bool { return out.TopLootedItems[i].TotalValue > out.TopLootedItems[j].TotalValue })
	if len(out.TopLootedItems) > 15 {
		out.TopLootedItems = out.TopLootedItems[:15]
	}
	out.NetLootValueEst = directKinah + int64(math.Round(float64(grossMarket)*(1.0-domain.DefaultMarketTaxRate)))

	return out, nil
}
