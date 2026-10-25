package agents

import (
	"maps"
	"strconv"
	"time"
)

// Observation is an immutable and periodically published view of an agent's internal state.
type Observation struct {
	ID        string            `json:"id"` // string because JavaScript numbers can't represent every uint64
	UpdatedAt time.Time         `json:"updated_at"`
	Running   bool              `json:"running"`
	Portfolio PortfolioSummary  `json:"portfolio"`
	Policies  map[string]Policy `json:"policies"`
}

// publishObservation builds a new observation of the agent's state; must be called from the agent's Run() goroutine.
func (a *Agent) publishObservation(running bool) {
	lastPrices := make(map[string]int64, len(a.symbols))
	for _, symbol := range a.symbols {
		lastPrices[symbol] = a.feeds[symbol].LastPrice()
	}

	a.observation.Store(&Observation{
		ID:        strconv.FormatUint(uint64(a.id), 10),
		UpdatedAt: time.Now().UTC(),
		Running:   running,
		Portfolio: a.portfolio.Summary(lastPrices),
		Policies:  maps.Clone(a.policies),
	})
}

// Observe returns the latest published observation (which is shared and must not be modified) or nil.
func (a *Agent) Observe() *Observation {
	return a.observation.Load()
}
