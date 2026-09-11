package strategies

import (
	"math/rand/v2"

	"github.com/knqu/goexchange/internal/agents"
	"github.com/knqu/goexchange/internal/engine"
)

type Noise struct {
	Rate      float64 // probability of generating an order on each tick (0-1)
	Size      int64   // base quantity for each order (scaled by policy.RiskAppetite)
	SeedPrice int64   // base price for first tick (used until market has both sides)
	Band      int64   // max price offset from base price (cannot be 0)
	rng       *rand.Rand
}

func NewNoise(rate float64, size, seedPrice, band int64, seed uint64) *Noise {
	return &Noise{
		Rate:      rate,
		Size:      size,
		SeedPrice: seedPrice,
		Band:      band,
		rng:       rand.New(rand.NewPCG(seed, 0)),
	}
}

func (n *Noise) OnTick(market agents.MarketSnapshot, policy agents.Policy, position int64, resting []engine.OrderID) []agents.Action {
	if n.rng.Float64() > n.Rate {
		return nil
	}

	base := n.SeedPrice
	if market.HasBothSides() {
		base = market.Mid()
	}

	side := engine.Side(n.rng.IntN(2))
	price := base + (int64(n.rng.IntN(int(2*n.Band+1))) - n.Band)
	quantity := int64(float64(n.Size) * policy.RiskAppetite)
	if quantity < 1 {
		return nil
	}

	return []agents.Action{{Type: agents.ActionSubmit, Side: side, OrderType: engine.Limit, TIF: engine.Day, Price: price, Quantity: quantity}}
}
