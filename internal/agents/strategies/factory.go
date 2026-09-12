package strategies

import (
	"fmt"
	"hash/fnv"

	"github.com/knqu/goexchange/internal/agents"
	"github.com/knqu/goexchange/internal/engine"
)

// New creates a new strategy instance by matching the correct strategy's constructor and applying given parameters.
func New(strategy string, params agents.StrategyParams, agentID engine.AgentID, symbol string) (agents.Strategy, error) {
	switch strategy {
	case "noise":
		return NewNoise(params.Rate, params.Size, params.SeedPrice, params.Band, seed(agentID, symbol)), nil
	case "marketmaker":
		return NewMarketMaker(params.HalfSpread, params.Size, params.MaxPosition, params.SkewPerLot), nil
	}
	return nil, fmt.Errorf("unknown strategy %q", strategy)
}

// seed derives an agent and symbol specific RNG seed, so runs are reproducible without instances moving in lockstep.
func seed(agentID engine.AgentID, symbol string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(symbol))
	return h.Sum64() ^ uint64(agentID)
}
