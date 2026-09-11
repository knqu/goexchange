package agents

import (
	"context"
	"log"
	"time"

	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/execution"
	"github.com/knqu/goexchange/internal/feed"
)

// --- agent setup and lifecycle ---

type Agent struct {
	id        engine.AgentID
	symbols   []string
	portfolio *Portfolio

	brain      Brain
	policies   map[string]Policy
	strategies map[string]Strategy

	feeds map[string]*feed.Client // per-symbol feeds (from accumulator)
	fills <-chan execution.Fill   // agent-wide fills (from distributor)
	gw    *GatewayClient

	resting map[string]map[engine.OrderID]int64 // remaining quantity for resting orders (allows agents to cancel)
}

// NewAgent initializes a new agent; defaultPolicy is a placeholder policy seeded before the first Brain.Think().
func NewAgent(id engine.AgentID, symbols []string, startingCash int64, brain Brain, defaultPolicy Policy, strategies map[string]Strategy, feeds map[string]*feed.Client, fills <-chan execution.Fill, gw *GatewayClient) *Agent {
	agent := &Agent{
		id:         id,
		symbols:    symbols,
		portfolio:  NewPortfolio(startingCash),
		brain:      brain,
		policies:   make(map[string]Policy, len(symbols)),
		strategies: strategies,
		feeds:      feeds,
		fills:      fills,
		gw:         gw,
		resting:    make(map[string]map[engine.OrderID]int64, len(symbols)),
	}

	for _, symbol := range symbols {
		agent.resting[symbol] = make(map[engine.OrderID]int64)
		agent.policies[symbol] = defaultPolicy // before Brain.Think() actually generates its first policy
	}

	return agent
}

// Run starts the agent's main loop, which calls Strategy.OnTick() and Brain.Think() at the given fast/slow intervals.
func (a *Agent) Run(ctx context.Context, fastTick time.Duration, slowTick time.Duration) {
	fast := time.NewTicker(fastTick)
	slow := time.NewTicker(slowTick)
	defer fast.Stop()
	defer slow.Stop()

	for {
		select {
		case <-fast.C: // trading loop
			a.tick()
		case <-slow.C: // policy deliberation loop
			a.think(ctx)
		case fill, ok := <-a.fills: // update portfolio and resting orders when a new fill is received
			if !ok {
				a.fills = nil
				continue
			}

			a.portfolio.OnFill(fill)

			if remaining, ok := a.resting[fill.Symbol][fill.OrderID]; ok {
				if remaining -= fill.Quantity; remaining <= 0 {
					delete(a.resting[fill.Symbol], fill.OrderID)
				} else {
					a.resting[fill.Symbol][fill.OrderID] = remaining
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// --- decision-making loops ---

func (a *Agent) tick() {
	for _, symbol := range a.symbols {
		policy := a.policies[symbol]

		if !policy.Participation {
			continue
		}

		snapshot := a.snapshot(symbol)

		for _, action := range a.strategies[symbol].OnTick(snapshot, policy) {
			id, err := a.gw.Do(symbol, action)
			if err != nil {
				log.Printf("agent %d: %v", a.id, err)
				continue
			}

			switch action.Type {
			case ActionSubmit:
				a.resting[symbol][id] = action.Quantity
			case ActionCancel:
				delete(a.resting[symbol], action.CancelID)
			}
		}
	}
}

func (a *Agent) think(ctx context.Context) {
	markets := make(map[string]MarketSnapshot, len(a.symbols))
	lastPrices := make(map[string]int64, len(a.symbols))

	for _, symbol := range a.symbols {
		markets[symbol] = a.snapshot(symbol)
		lastPrices[symbol] = markets[symbol].LastPrice
	}

	summary := a.portfolio.Summary(lastPrices)

	policies, err := a.brain.Think(ctx, markets, summary)
	if err != nil {
		return // keep existing policies as-is
	}

	for symbol, policy := range policies {
		a.policies[symbol] = policy
	}
}

// --- helpers ---

func (a *Agent) snapshot(symbol string) MarketSnapshot {
	snapshot := MarketSnapshot{
		Symbol:       symbol,
		LastPrice:    a.feeds[symbol].LastPrice(),
		Depth:        a.feeds[symbol].Book(),
		RecentTrades: a.feeds[symbol].Recent(),
	}

	return snapshot
}
