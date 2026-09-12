package strategies

import (
	"github.com/knqu/goexchange/internal/agents"
	"github.com/knqu/goexchange/internal/engine"
)

type MarketMaker struct {
	HalfSpread  int64   // ticks away from fair value to quote
	Size        int64   // max quantity for each order (scaled by policy.RiskAppetite)
	MaxPosition int64   // threshold to stop quoting (limits inventory risk)
	SkewPerLot  float64 // ticks to shift quotes per unit of inventory (mitigates inventory risk)
}

func NewMarketMaker(halfSpread, size, maxPosition int64, skewPerLot float64) *MarketMaker {
	return &MarketMaker{
		HalfSpread:  halfSpread,
		Size:        size,
		MaxPosition: maxPosition,
		SkewPerLot:  skewPerLot,
	}
}

func (m *MarketMaker) OnTick(market agents.MarketSnapshot, policy agents.Policy, position int64, resting []engine.OrderID) []agents.Action {
	if !market.HasBothSides() {
		return nil // nothing to quote around
	}

	actions := make([]agents.Action, 0, len(resting)+2)

	// issue cancels for existing quotes before re-quoting
	for _, orderID := range resting {
		actions = append(actions, agents.NewCancel(orderID))
	}

	// calculate quoted price from market bid/ask, conviction, and inventory risk mitigations
	fair := market.Mid() + int64(policy.Bias*float64(m.HalfSpread)) - int64(float64(position)*m.SkewPerLot)

	size := int64(float64(m.Size) * policy.RiskAppetite)
	if size < 1 {
		return actions // still issue cancels but don't re-quote
	}

	if position < m.MaxPosition {
		actions = append(actions, agents.NewLimitAt(engine.Buy, fair-m.HalfSpread, size))
	}

	if position > -m.MaxPosition {
		actions = append(actions, agents.NewLimitAt(engine.Sell, fair+m.HalfSpread, size))
	}

	return actions
}
