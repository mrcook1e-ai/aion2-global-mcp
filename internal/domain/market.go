package domain

import "time"

// MarketQuote represents live auction house data on a specific AION 2 Global server.
type MarketQuote struct {
	ItemID    ItemID    `json:"itemId"`
	ServerID  ServerID  `json:"serverId"`
	Market    string    `json:"market,omitempty"`
	MinPrice  int64     `json:"minPrice"`
	Median7d  int64     `json:"median7d"`
	Low30d    int64     `json:"low30d,omitempty"`
	High30d   int64     `json:"high30d,omitempty"`
	Listings  int       `json:"listings"`
	Change24h float64   `json:"change24h,omitempty"`
	Change7d  float64   `json:"change7d,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// EffectiveUnitPrice returns the most reliable unit price and whether an active listing exists.
// When active listings exist, MinPrice is used unless it is a clear outlier (>3x Median7d on thin volume),
// in which case callers can inspect SpikeWarning. When Listings == 0, Median7d is used as historical reference.
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
