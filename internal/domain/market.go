package domain

import (
	"math"
	"time"
)

const (
	// StaleQuoteMinutes flags crowdsourced quotes older than 2.5 hours as potentially stale.
	StaleQuoteMinutes = 150
	// SlippageSpreadThreshold (35%) flags quotes where minPrice diverges sharply from median7d.
	SlippageSpreadThreshold = 0.35
)

// MarketQuote represents live auction house data on a specific AION 2 Global server.
type MarketQuote struct {
	ItemID          ItemID    `json:"itemId"`
	ServerID        ServerID  `json:"serverId"`
	Market          string    `json:"market,omitempty"`
	MinPrice        int64     `json:"minPrice"`
	Median7d        int64     `json:"median7d"`
	Low30d          int64     `json:"low30d,omitempty"`
	High30d         int64     `json:"high30d,omitempty"`
	Listings        int       `json:"listings"`
	Change24h       float64   `json:"change24h,omitempty"`
	Change7d        float64   `json:"change7d,omitempty"`
	QuoteAgeMinutes int       `json:"quoteAgeMinutes"`
	StaleQuote      bool      `json:"staleQuote,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// HydrateFreshness populates QuoteAgeMinutes and StaleQuote relative to now.
func (q *MarketQuote) HydrateFreshness(now time.Time) {
	if q == nil || q.UpdatedAt.IsZero() {
		return
	}
	age := int(now.Sub(q.UpdatedAt).Minutes())
	if age < 0 {
		age = 0
	}
	q.QuoteAgeMinutes = age
	q.StaleQuote = age >= StaleQuoteMinutes
}

// SpreadVsMedianPct returns ((MinPrice - Median7d) / Median7d) * 100 when both are positive.
func (q *MarketQuote) SpreadVsMedianPct() float64 {
	if q == nil || q.MinPrice <= 0 || q.Median7d <= 0 {
		return 0
	}
	diff := (float64(q.MinPrice) - float64(q.Median7d)) / float64(q.Median7d) * 100.0
	return math.Round(diff*10) / 10
}

// HasSlippageRisk reports whether MinPrice diverges by >=35% from Median7d (single under/overpriced lot risk).
func (q *MarketQuote) HasSlippageRisk() bool {
	if q == nil || q.MinPrice <= 0 || q.Median7d <= 0 {
		return false
	}
	ratio := math.Abs(float64(q.MinPrice)-float64(q.Median7d)) / float64(q.Median7d)
	return ratio >= SlippageSpreadThreshold
}

// EffectiveUnitPrice returns the most reliable unit price and whether an active listing exists.
func (q *MarketQuote) EffectiveUnitPrice() (price int64, active bool) {
	if q == nil {
		return 0, false
	}
	if q.Listings > 0 && q.MinPrice > 0 {
		return q.MinPrice, true
	}
	if q.Median7d > 0 {
		return q.Median7d, false
	}
	if q.MinPrice > 0 {
		return q.MinPrice, false
	}
	return 0, false
}

// ConservativeSellPrice protects profit calculations from single overpriced troll listings:
// if MinPrice exceeds 1.8x of Median7d (when Median7d > 0), we cap expected sell price at Median7d.
func (q *MarketQuote) ConservativeSellPrice() (price int64, cappedToMedian bool, hasData bool) {
	if q == nil {
		return 0, false, false
	}
	if q.Listings > 0 && q.MinPrice > 0 {
		if q.Median7d > 0 && float64(q.MinPrice) > float64(q.Median7d)*1.8 {
			return q.Median7d, true, true
		}
		return q.MinPrice, false, true
	}
	if q.Median7d > 0 {
		return q.Median7d, true, true
	}
	return 0, false, false
}

// MarketDeal highlights an actionable buy-dip / flip or sell-peak opportunity on a server's Auction House.
type MarketDeal struct {
	ItemID              ItemID  `json:"itemId"`
	ItemName            string  `json:"itemName"`
	MainCategory        string  `json:"mainCategory,omitempty"`
	SubCategory         string  `json:"subCategory,omitempty"`
	Grade               int     `json:"grade,omitempty"`
	Signal              string  `json:"signal"` // "dip_buy" or "peak_sell"
	MinPrice            int64   `json:"minPrice"`
	Median7d            int64   `json:"median7d"`
	Low30d              int64   `json:"low30d,omitempty"`
	High30d             int64   `json:"high30d,omitempty"`
	Listings            int     `json:"listings"`
	SpreadVsMedianPct   float64 `json:"spreadVsMedianPct"`             // negative = discount below median7d
	FlipNetProfitPerOne int64   `json:"flipNetProfitPerOne,omitempty"` // round(median7d*(1-tax)) - minPrice
	FlipMarginPct       float64 `json:"flipMarginPct,omitempty"`
	OwnedQty            int64   `json:"ownedQty,omitempty"` // player's bag + storage stock
	Change24h           float64 `json:"change24h,omitempty"`
	Change7d            float64 `json:"change7d,omitempty"`
	QuoteAgeMinutes     int     `json:"quoteAgeMinutes"`
	StaleQuote          bool    `json:"staleQuote,omitempty"`
}

// ServerQuoteBrief summarizes one server's price for cross-server comparisons.
type ServerQuoteBrief struct {
	ServerID        ServerID `json:"serverId"`
	Market          string   `json:"market,omitempty"`
	MinPrice        int64    `json:"minPrice"`
	Median7d        int64    `json:"median7d"`
	Listings        int      `json:"listings"`
	QuoteAgeMinutes int      `json:"quoteAgeMinutes"`
}

// ItemServerComparison compares an item's market price on a home server against peer servers cached in SQLite.
type ItemServerComparison struct {
	ItemID            ItemID             `json:"itemId"`
	ItemName          string             `json:"itemName"`
	HomeServerID      ServerID           `json:"homeServerId"`
	HomeMinPrice      int64              `json:"homeMinPrice"`
	HomeMedian7d      int64              `json:"homeMedian7d"`
	HomeListings      int                `json:"homeListings"`
	ServersCount      int                `json:"serversCount"`
	RegionalMinPrice  int64              `json:"regionalMinPrice"`
	CheapestServerID  ServerID           `json:"cheapestServerId"`
	RegionalMaxPrice  int64              `json:"regionalMaxPrice"`
	HighestServerID   ServerID           `json:"highestServerId"`
	RegionalMedianMin int64              `json:"regionalMedianMin"`
	HomeVsRegionalPct float64            `json:"homeVsRegionalPct"`
	TargetServers     []ServerQuoteBrief `json:"targetServers,omitempty"`
}
