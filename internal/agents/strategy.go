package agents

import (
	"context"
)

// Strategy is the agent's derived (fast) decision-making loop for one symbol, acting on market data and set policy.
type Strategy interface {
	OnTick(market MarketSnapshot, policy Policy) []Action
}

// Brain is the agent's deliberate (slow) decision-making loop, adjusting policies based on market and portfolio data.
type Brain interface {
	Think(ctx context.Context, markets map[string]MarketSnapshot, portfolio PortfolioSummary) (map[string]Policy, error)
}
