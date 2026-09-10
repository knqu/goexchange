package agents

import "context"

// Stub implements Brain with no LLM integration.
type Stub struct {
	Policies map[string]Policy
}

// Think returns a nil policies map, leaving agents on whatever default policy they were seeded with at creation.
func (s Stub) Think(ctx context.Context, markets map[string]MarketSnapshot, portfolio PortfolioSummary) (map[string]Policy, error) {
	return s.Policies, nil
}
