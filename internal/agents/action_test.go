package agents

import (
	"testing"

	"github.com/knqu/goexchange/internal/engine"
)

// TestNewLimit checks that aggression scales order prices across the spread, and that crossing orders are marked IOC.
func TestNewLimit(t *testing.T) {
	market := MarketSnapshot{Depth: engine.DepthSnapshot{
		Bids: []engine.PriceLevel{{Price: 9948, Quantity: 100}},
		Asks: []engine.PriceLevel{{Price: 9952, Quantity: 100}},
	}} // spread = 4

	cases := []struct {
		name       string
		side       engine.Side
		aggression float64
		wantPrice  int64
		wantTIF    engine.TIF
	}{
		// aggression = 0: orders should rest at their own side's top of book
		{"buy_passive_rests_at_bid", engine.Buy, 0, 9948, engine.Day},
		{"sell_passive_rests_at_ask", engine.Sell, 0, 9952, engine.Day},

		// 0 < aggression < 0.5: price remains within the spread, so orders should continue resting
		{"buy_inside_spread_rests", engine.Buy, 0.25, 9950, engine.Day},
		{"sell_inside_spread_rests", engine.Sell, 0.25, 9950, engine.Day},

		// aggression = 0.5: offset now covers the entire spread, reaching the other side's top of book; should be IOC
		{"buy_at_ask_crosses", engine.Buy, 0.5, 9952, engine.IOC},
		{"sell_at_bid_crosses", engine.Sell, 0.5, 9948, engine.IOC},

		// aggression > 0.5: price sweeps beyond the other side's top of book, reaching deeper levels; should be IOC
		{"buy_through_ask_crosses", engine.Buy, 1.0, 9956, engine.IOC},
		{"sell_through_bid_crosses", engine.Sell, 1.0, 9944, engine.IOC},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewLimit(tc.side, 10, market, Policy{Aggression: tc.aggression})

			if got.Price != tc.wantPrice {
				t.Errorf("price = %d, want %d", got.Price, tc.wantPrice)
			}
			if got.TIF != tc.wantTIF {
				t.Errorf("tif = %v, want %v", got.TIF, tc.wantTIF)
			}
			if got.Type != ActionSubmit || got.OrderType != engine.Limit {
				t.Errorf("got %+v, want a limit submit", got)
			}
			if got.Side != tc.side || got.Quantity != 10 {
				t.Errorf("side/quantity = %v/%d, want %v/10", got.Side, got.Quantity, tc.side)
			}
		})
	}
}
