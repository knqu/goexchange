package strategies

import (
	"github.com/knqu/goexchange/internal/agents"
	"github.com/knqu/goexchange/internal/engine"
)

type MarketMaker struct {
	HalfSpread  int64 // ticks away from mid to quote
	Size        int64 // base quantity for each order (scaled by policy.RiskAppetite)
	SkewPerLot  int64 // ticks to shift quotes per unit of inventory (inventory risk mitigation)
	MaxPosition int64 // threshold to stop quoting (secondary inventory risk mitigation)
}

func NewMarketMaker(halfSpread, size, skewPerLot, maxPosition int64) *MarketMaker {
	return &MarketMaker{
		HalfSpread:  halfSpread,
		Size:        size,
		SkewPerLot:  skewPerLot,
		MaxPosition: maxPosition,
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
	fair := market.Mid() + int64(policy.Bias*float64(m.HalfSpread)) - (position * m.SkewPerLot)

	size := int64(float64(m.Size) * policy.RiskAppetite)
	if size < 1 {
		return actions // still issue cancels but but don't re-quote
	}

	if position < m.MaxPosition {
		actions = append(actions, agents.NewLimitAt(engine.Buy, fair-m.HalfSpread, size))
	}

	if position > -m.MaxPosition {
		actions = append(actions, agents.NewLimitAt(engine.Sell, fair+m.HalfSpread, size))
	}

	return actions
}
