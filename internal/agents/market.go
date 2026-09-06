package agents

import (
	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/feed"
)

// MarketSnapshot contains market data for a single symbol at a discrete tick.
type MarketSnapshot struct {
	Symbol       string
	LastPrice    int64 // derived from last confirmed trade
	Depth        engine.DepthSnapshot
	RecentTrades []feed.Trade
}

// BestBid returns the best bid price, or 0 if there are no bids.
func (m MarketSnapshot) BestBid() int64 {
	if len(m.Depth.Bids) == 0 {
		return 0
	}
	return m.Depth.Bids[0].Price
}

// BestAsk returns the best ask price, or 0 if there are no asks.
func (m MarketSnapshot) BestAsk() int64 {
	if len(m.Depth.Asks) == 0 {
		return 0
	}
	return m.Depth.Asks[0].Price
}

// Mid returns the price between the best bid and ask; caller must check that MarketSnapshot.HasBothSides() is true.
func (m MarketSnapshot) Mid() int64 {
	return (m.BestBid() + m.BestAsk()) / 2
}

// HasBothSides returns true if the market snapshot has at least one bid and one ask.
func (m MarketSnapshot) HasBothSides() bool {
	return len(m.Depth.Bids) > 0 && len(m.Depth.Asks) > 0
}
