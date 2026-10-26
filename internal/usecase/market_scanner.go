package usecase

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type MarketDealsQuery struct {
	Character      string
	ServerID       int
	Category       string  // Optional mainCategory or subCategory (e.g. "craftresource", "misc", "magicstone")
	MinListings    int     // Default 15
	MinDiscountPct float64 // Default 20.0 (20% below median7d)
	TaxRate        float64 // Default 0.12
	RefreshSample  bool    // Refresh owned tradable items + category sample before scanning
	Limit          int     // Default 20 per bucket
}

type MarketDealsReport struct {
	CharacterKey string              `json:"characterKey,omitempty"`
	ServerID     domain.ServerID     `json:"serverId"`
	ScannedItems int                 `json:"scannedItems"`
	DipBuys      []domain.MarketDeal `json:"dipBuys"`
	PeakSells    []domain.MarketDeal `json:"peakSells"`
}

type CompareServersQuery struct {
	Query           string
	ItemIDs         []int64
	Character       string
	HomeServerID    int
	TargetServerIDs []int
	ForceRefresh    bool
	Limit           int
}

type CompareServersReport struct {
	HomeServerID domain.ServerID               `json:"homeServerId"`
	Comparisons  []domain.ItemServerComparison `json:"comparisons"`
}

type MarketScannerService struct {
	catalog  domain.CatalogRepository
	market   domain.MarketRepository
	invRepo  domain.InventoryRepository
	charSvc  *CharacterService
	syncSvc  *SyncService
	resolver *Resolver
}

func NewMarketScannerService(
	catalog domain.CatalogRepository,
	market domain.MarketRepository,
	invRepo domain.InventoryRepository,
	charSvc *CharacterService,
	syncSvc *SyncService,
	resolver *Resolver,
) *MarketScannerService {
	return &MarketScannerService{
		catalog:  catalog,
		market:   market,
		invRepo:  invRepo,
		charSvc:  charSvc,
		syncSvc:  syncSvc,
		resolver: resolver,
	}
}

func (s *MarketScannerService) ScanMarketDeals(ctx context.Context, q MarketDealsQuery) (*MarketDealsReport, error) {
	_, _ = s.syncSvc.SyncTelemetry(ctx)

	var (
		charKey string
		srvID   = domain.ServerID(q.ServerID)
	)
	if ch, err := s.charSvc.FindCharacter(ctx, q.Character); err == nil && ch != nil {
		charKey = ch.Key
		if srvID == 0 {
			srvID = ch.ServerID
		}
	}
	if srvID <= 0 {
		return nil, fmt.Errorf("не удалось определить serverId для сканирования рынка")
	}

	owned := make(map[domain.ItemID]int64)
	var ownedTradableIDs []domain.ItemID
	if charKey != "" {
		if agg, err := s.invRepo.GetAggregatedInventory(ctx, charKey); err == nil {
			var allInvIDs []domain.ItemID
			for id, a := range agg {
				owned[id] = a.TotalCount
				allInvIDs = append(allInvIDs, id)
			}
			meta, _ := s.resolver.ResolveItems(ctx, allInvIDs)
			for _, id := range allInvIDs {
				if domain.IsKinahItem(id) {
					continue
				}
				if it, ok := meta[id]; ok && it.Tradable != nil && *it.Tradable {
					ownedTradableIDs = append(ownedTradableIDs, id)
				}
			}
		}
	}

	if q.RefreshSample {
		refreshIDs := append([]domain.ItemID{}, ownedTradableIDs...)
		cat := q.Category
		if cat == "" {
			cat = "misc"
		}
		if sample, err := s.catalog.SearchItems(ctx, "", cat, 0, 60); err == nil {
			for _, it := range sample {
				if it.Tradable != nil && *it.Tradable {
					refreshIDs = append(refreshIDs, it.ID)
				}
			}
		}
		if len(refreshIDs) > 90 {
			refreshIDs = refreshIDs[:90]
		}
		_, _ = s.resolver.EnsureMarketQuotes(ctx, srvID, refreshIDs, false)
	} else if len(ownedTradableIDs) > 0 {
		// Passively ensure cached quotes exist for player's tradable stock (respecting 25m TTL)
		sample := ownedTradableIDs
		if len(sample) > 60 {
			sample = sample[:60]
		}
		_, _ = s.resolver.EnsureMarketQuotes(ctx, srvID, sample, false)
	}

	minListings := q.MinListings
	if minListings <= 0 {
		minListings = 15
	}
	minDisc := q.MinDiscountPct
	if minDisc <= 0 {
		minDisc = 20.0
	}
	tax := q.TaxRate
	if tax <= 0 || tax >= 1 {
		tax = domain.DefaultMarketTaxRate
	}
	limit := q.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	rows, err := s.market.ListServerMarketWithItems(ctx, srvID, q.Category, minListings)
	if err != nil {
		return nil, err
	}

	var dips, peaks []domain.MarketDeal
	for _, r := range rows {
		mq := r.Quote
		if mq.MinPrice <= 0 || mq.Median7d <= 0 {
			continue
		}
		spread := mq.SpreadVsMedianPct() // negative = below median7d

		// 1. Dip buy / flip candidate
		if spread <= -minDisc {
			netResell := int64(math.Round(float64(mq.Median7d)*(1.0-tax))) - mq.MinPrice
			var flipMargin float64
			if mq.MinPrice > 0 {
				flipMargin = math.Round(float64(netResell)/float64(mq.MinPrice)*1000) / 10
			}
			dips = append(dips, domain.MarketDeal{
				ItemID:              r.Item.ID,
				ItemName:            r.Item.Name,
				MainCategory:        r.Item.MainCategory,
				SubCategory:         r.Item.SubCategory,
				Grade:               r.Item.Grade,
				Signal:              "dip_buy",
				MinPrice:            mq.MinPrice,
				Median7d:            mq.Median7d,
				Low30d:              mq.Low30d,
				High30d:             mq.High30d,
				Listings:            mq.Listings,
				SpreadVsMedianPct:   spread,
				FlipNetProfitPerOne: netResell,
				FlipMarginPct:       flipMargin,
				OwnedQty:            owned[r.Item.ID],
				Change24h:           mq.Change24h,
				Change7d:            mq.Change7d,
				QuoteAgeMinutes:     mq.QuoteAgeMinutes,
				StaleQuote:          mq.StaleQuote,
			})
		}

		// 2. Peak sell candidate (near 30d high or >= +20% above 7d median).
		// Ignore single troll listings: a real peak must stay within ~10% of the 30d high and <= +150% above median7d.
		withinHigh30 := mq.High30d <= 0 || float64(mq.MinPrice) <= 1.10*float64(mq.High30d)
		nearHigh30 := mq.High30d > 0 && float64(mq.MinPrice) >= 0.88*float64(mq.High30d) && spread >= 12.0
		if (spread >= 20.0 || nearHigh30) && spread <= 150.0 && withinHigh30 {
			peaks = append(peaks, domain.MarketDeal{
				ItemID:            r.Item.ID,
				ItemName:          r.Item.Name,
				MainCategory:      r.Item.MainCategory,
				SubCategory:       r.Item.SubCategory,
				Grade:             r.Item.Grade,
				Signal:            "peak_sell",
				MinPrice:          mq.MinPrice,
				Median7d:          mq.Median7d,
				Low30d:            mq.Low30d,
				High30d:           mq.High30d,
				Listings:          mq.Listings,
				SpreadVsMedianPct: spread,
				OwnedQty:          owned[r.Item.ID],
				Change24h:         mq.Change24h,
				Change7d:          mq.Change7d,
				QuoteAgeMinutes:   mq.QuoteAgeMinutes,
				StaleQuote:        mq.StaleQuote,
			})
		}
	}

	sort.Slice(dips, func(i, j int) bool {
		// Prioritize non-stale liquid dips with positive flip margin, then deepest discount
		if dips[i].StaleQuote != dips[j].StaleQuote {
			return !dips[i].StaleQuote
		}
		return dips[i].SpreadVsMedianPct < dips[j].SpreadVsMedianPct
	})
	if len(dips) > limit {
		dips = dips[:limit]
	}

	sort.Slice(peaks, func(i, j int) bool {
		// Prioritize items the player actually owns in bag/storage first!
		iOwned := peaks[i].OwnedQty > 0
		jOwned := peaks[j].OwnedQty > 0
		if iOwned != jOwned {
			return iOwned
		}
		return peaks[i].SpreadVsMedianPct > peaks[j].SpreadVsMedianPct
	})
	if len(peaks) > limit {
		peaks = peaks[:limit]
	}

	return &MarketDealsReport{
		CharacterKey: charKey,
		ServerID:     srvID,
		ScannedItems: len(rows),
		DipBuys:      dips,
		PeakSells:    peaks,
	}, nil
}

func (s *MarketScannerService) CompareServerPrices(ctx context.Context, q CompareServersQuery) (*CompareServersReport, error) {
	homeSrv := domain.ServerID(q.HomeServerID)
	if homeSrv <= 0 {
		if ch, err := s.charSvc.FindCharacter(ctx, q.Character); err == nil && ch != nil {
			homeSrv = ch.ServerID
		}
	}
	if homeSrv <= 0 {
		return nil, fmt.Errorf("укажи homeServerId или выбери локального персонажа")
	}

	limit := q.Limit
	if limit <= 0 || limit > 25 {
		limit = 10
	}

	var itemIDs []domain.ItemID
	for _, rawID := range q.ItemIDs {
		if rawID > 0 {
			itemIDs = append(itemIDs, domain.ItemID(rawID))
		}
	}
	if q.Query != "" {
		if exactID, err := strconv.ParseInt(strings.TrimSpace(q.Query), 10, 64); err == nil && exactID > 0 {
			itemIDs = append(itemIDs, domain.ItemID(exactID))
		} else {
			found, err := s.catalog.SearchItems(ctx, q.Query, "", 0, limit)
			if err != nil {
				return nil, err
			}
			for _, it := range found {
				itemIDs = append(itemIDs, it.ID)
			}
		}
	}
	itemIDs = deduplicateItemIDs(itemIDs)
	if len(itemIDs) == 0 {
		return nil, fmt.Errorf("не найдено предметов для сравнения — передай query или itemIds")
	}
	if len(itemIDs) > limit {
		itemIDs = itemIDs[:limit]
	}

	itemsMap, _ := s.resolver.ResolveItems(ctx, itemIDs)
	_, _ = s.resolver.EnsureMarketQuotes(ctx, homeSrv, itemIDs, q.ForceRefresh)

	multiMap, err := s.market.GetMultiServerQuotes(ctx, itemIDs)
	if err != nil {
		return nil, err
	}

	targetSet := make(map[domain.ServerID]bool, len(q.TargetServerIDs))
	for _, tid := range q.TargetServerIDs {
		if tid > 0 {
			targetSet[domain.ServerID(tid)] = true
		}
	}

	// Default peer servers: same regional prefix (e.g. 23xx for EU 2319) when TargetServerIDs is omitted
	homePrefix := int(homeSrv) / 100

	var comps []domain.ItemServerComparison
	for _, iid := range itemIDs {
		quotes := multiMap[iid]
		comp := domain.ItemServerComparison{
			ItemID:       iid,
			ItemName:     s.resolver.ItemLabel(itemsMap, iid),
			HomeServerID: homeSrv,
		}

		var regionalPrices []int64
		for _, mq := range quotes {
			if mq.ServerID == homeSrv {
				comp.HomeMinPrice = mq.MinPrice
				comp.HomeMedian7d = mq.Median7d
				comp.HomeListings = mq.Listings
			}
			inSameRegion := len(targetSet) > 0 && targetSet[mq.ServerID] || (len(targetSet) == 0 && int(mq.ServerID)/100 == homePrefix)
			if !inSameRegion {
				continue
			}
			if mq.Listings > 0 && mq.MinPrice > 0 {
				comp.ServersCount++
				regionalPrices = append(regionalPrices, mq.MinPrice)
				if comp.RegionalMinPrice == 0 || mq.MinPrice < comp.RegionalMinPrice {
					comp.RegionalMinPrice = mq.MinPrice
					comp.CheapestServerID = mq.ServerID
				}
				if mq.MinPrice > comp.RegionalMaxPrice {
					comp.RegionalMaxPrice = mq.MinPrice
					comp.HighestServerID = mq.ServerID
				}
			}
			if len(targetSet) > 0 && targetSet[mq.ServerID] {
				comp.TargetServers = append(comp.TargetServers, domain.ServerQuoteBrief{
					ServerID:        mq.ServerID,
					Market:          mq.Market,
					MinPrice:        mq.MinPrice,
					Median7d:        mq.Median7d,
					Listings:        mq.Listings,
					QuoteAgeMinutes: mq.QuoteAgeMinutes,
				})
			}
		}

		if len(regionalPrices) > 0 {
			sort.Slice(regionalPrices, func(i, j int) bool { return regionalPrices[i] < regionalPrices[j] })
			comp.RegionalMedianMin = regionalPrices[len(regionalPrices)/2]
			if comp.HomeMinPrice > 0 && comp.RegionalMedianMin > 0 {
				diff := (float64(comp.HomeMinPrice) - float64(comp.RegionalMedianMin)) / float64(comp.RegionalMedianMin) * 100.0
				comp.HomeVsRegionalPct = math.Round(diff*10) / 10
			}
		}

		// If no explicit TargetServerIDs were passed, include up to 6 cheapest/peer servers in the region for context
		if len(targetSet) == 0 {
			var peers []domain.ServerQuoteBrief
			for _, mq := range quotes {
				if int(mq.ServerID)/100 == homePrefix && mq.Listings > 0 && mq.MinPrice > 0 {
					peers = append(peers, domain.ServerQuoteBrief{
						ServerID:        mq.ServerID,
						Market:          mq.Market,
						MinPrice:        mq.MinPrice,
						Median7d:        mq.Median7d,
						Listings:        mq.Listings,
						QuoteAgeMinutes: mq.QuoteAgeMinutes,
					})
				}
			}
			sort.Slice(peers, func(i, j int) bool { return peers[i].MinPrice < peers[j].MinPrice })
			if len(peers) > 8 {
				peers = peers[:8]
			}
			comp.TargetServers = peers
		}

		comps = append(comps, comp)
	}

	return &CompareServersReport{
		HomeServerID: homeSrv,
		Comparisons:  comps,
	}, nil
}
