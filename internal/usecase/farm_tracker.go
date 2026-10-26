package usecase

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type MapFarmSummary struct {
	MapID         domain.MapID      `json:"mapId"`
	MapName       string            `json:"mapName"`
	DurationMin   float64           `json:"durationMin"`
	DirectKinah   int64             `json:"directKinah"`
	GrossLootVal  int64             `json:"grossLootVal"`
	NetValueAfter int64             `json:"netValueAfterTax"`
	KinahPerHour  int64             `json:"kinahPerHour"`
	TopItems      []ValuedLootEntry `json:"topItems"`
}

type ValuedLootEntry struct {
	ItemID     domain.ItemID `json:"itemId"`
	Name       string        `json:"name"`
	Count      int64         `json:"count"`
	UnitPrice  int64         `json:"unitPrice"`
	TotalValue int64         `json:"totalValue"`
}

type FarmReport struct {
	ServerID       domain.ServerID  `json:"serverId"`
	DaysAnalyzed   int              `json:"daysAnalyzed"`
	TotalNetValue  int64            `json:"totalNetValue"`
	StretchesCount int              `json:"stretchesCount"`
	MapsBreakdown  []MapFarmSummary `json:"mapsBreakdown"`
}

type FarmService struct {
	localFS  domain.LocalTelemetryReader
	charSvc  *CharacterService
	resolver *Resolver
}

func NewFarmService(localFS domain.LocalTelemetryReader, charSvc *CharacterService, resolver *Resolver) *FarmService {
	return &FarmService{localFS: localFS, charSvc: charSvc, resolver: resolver}
}

func (s *FarmService) AnalyzeDropsValue(ctx context.Context, character string, serverID int, days int) (*FarmReport, error) {
	if s.localFS == nil || !s.localFS.Available() {
		return nil, fmt.Errorf("локальные данные Questlog недоступны")
	}
	if days <= 0 || days > 30 {
		days = 7
	}
	srvID := domain.ServerID(serverID)
	if srvID <= 0 {
		if ch, err := s.charSvc.FindCharacter(ctx, character); err == nil && ch != nil {
			srvID = ch.ServerID
		}
	}
	if srvID <= 0 {
		return nil, fmt.Errorf("не удалось определить serverId для оценки дропа по аукциону")
	}

	stretches, err := s.localFS.ReadDrops(ctx, days)
	if err != nil {
		return nil, err
	}

	var allIDs []domain.ItemID
	for _, st := range stretches {
		for _, it := range st.Items {
			allIDs = append(allIDs, it.ItemID)
		}
	}

	itemsByID, _ := s.resolver.ResolveItems(ctx, allIDs)

	// Strictly query the Auction House ONLY for items explicitly marked tradable == true (skipping NULL/0 soulbound drops & currencies!)
	var tradableIDs []domain.ItemID
	for _, id := range deduplicateItemIDs(allIDs) {
		if domain.IsKinahItem(id) {
			continue
		}
		if it, ok := itemsByID[id]; ok && it.Tradable != nil && *it.Tradable {
			tradableIDs = append(tradableIDs, id)
		}
	}

	quotes, err := s.resolver.EnsureMarketQuotes(ctx, srvID, tradableIDs, false)
	if err != nil {
		return nil, err
	}
	dungeons := s.resolver.EntityMap(ctx, domain.EntityDungeon)
	nightmares := s.resolver.EntityMap(ctx, domain.EntityNightmare)

	type mapAcc struct {
		mapID      domain.MapID
		durationS  float64
		itemCounts map[domain.ItemID]int64
	}
	byMap := make(map[domain.MapID]*mapAcc)

	for _, st := range stretches {
		acc, ok := byMap[st.MapID]
		if !ok {
			acc = &mapAcc{mapID: st.MapID, itemCounts: make(map[domain.ItemID]int64)}
			byMap[st.MapID] = acc
		}
		dur := st.To.Sub(st.From).Seconds()
		if dur > 0 {
			acc.durationS += dur
		}
		for _, it := range st.Items {
			acc.itemCounts[it.ItemID] += it.Count
		}
	}

	report := &FarmReport{
		ServerID:       srvID,
		DaysAnalyzed:   days,
		StretchesCount: len(stretches),
	}

	for mapID, acc := range byMap {
		mapName := fmt.Sprintf("Map #%d", mapID)
		if d, ok := dungeons[int64(mapID)]; ok && d.Name != "" {
			mapName = d.Name
		} else if n, ok := nightmares[int64(mapID)]; ok && n.Name != "" {
			mapName = n.Name
		}

		var directKinah, grossMarketLoot int64
		var valued []ValuedLootEntry
		for iid, cnt := range acc.itemCounts {
			if domain.IsKinahItem(iid) {
				directKinah += cnt
				valued = append(valued, ValuedLootEntry{
					ItemID:     iid,
					Name:       s.resolver.ItemLabel(itemsByID, iid),
					Count:      cnt,
					UnitPrice:  1,
					TotalValue: cnt,
				})
				continue
			}

			q := quotes[iid]
			price, _, _ := q.ConservativeSellPrice()
			if price <= 0 {
				continue
			}
			val := price * cnt
			grossMarketLoot += val
			valued = append(valued, ValuedLootEntry{
				ItemID:     iid,
				Name:       s.resolver.ItemLabel(itemsByID, iid),
				Count:      cnt,
				UnitPrice:  price,
				TotalValue: val,
			})
		}

		sort.Slice(valued, func(i, j int) bool { return valued[i].TotalValue > valued[j].TotalValue })
		if len(valued) > 10 {
			valued = valued[:10]
		}

		netLoot := int64(math.Round(float64(grossMarketLoot) * (1.0 - domain.DefaultMarketTaxRate)))
		netTotal := directKinah + netLoot
		durMin := math.Round(acc.durationS/60*10) / 10
		var kph int64
		if acc.durationS >= 60 {
			kph = int64(math.Round(float64(netTotal) / (acc.durationS / 3600.0)))
		}

		report.TotalNetValue += netTotal
		report.MapsBreakdown = append(report.MapsBreakdown, MapFarmSummary{
			MapID:         mapID,
			MapName:       mapName,
			DurationMin:   durMin,
			DirectKinah:   directKinah,
			GrossLootVal:  grossMarketLoot,
			NetValueAfter: netTotal,
			KinahPerHour:  kph,
			TopItems:      valued,
		})
	}

	sort.Slice(report.MapsBreakdown, func(i, j int) bool {
		return report.MapsBreakdown[i].NetValueAfter > report.MapsBreakdown[j].NetValueAfter
	})
	return report, nil
}
