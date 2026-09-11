package agents

import (
	"context"

	"github.com/knqu/goexchange/internal/engine"
)

// Strategy is the agent's derived (fast) decision-making loop, acting on market/portfolio data and set policy.
// One Strategy is created per symbol, as different symbols can exhibit different price actions.
type Strategy interface {
	OnTick(market MarketSnapshot, policy Policy, position int64, resting []engine.OrderID) []Action
}

// Brain is the agent's deliberate (slow) decision-making loop, adjusting policies based on market and portfolio data.
// One Brain is created per agent (shared across symbols), as actions may depend on portfolio-level information.
type Brain interface {
	Think(ctx context.Context, markets map[string]MarketSnapshot, portfolio PortfolioSummary) (map[string]Policy, error)
}
