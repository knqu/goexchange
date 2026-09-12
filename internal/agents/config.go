package agents

import (
	"fmt"
	"os"
	"time"

	"github.com/knqu/goexchange/internal/engine"
	"go.yaml.in/yaml/v4"
)

// --- fleet configuration data structures ---

type Fleet struct {
	Defaults Defaults      `yaml:"defaults"`
	Agents   []AgentConfig `yaml:"agents"`
}

type Defaults struct {
	Cash     int64         `yaml:"cash"`
	FastTick time.Duration `yaml:"fast_tick"`
	SlowTick time.Duration `yaml:"slow_tick"`
	Policy   DefaultPolicy `yaml:"policy"`
}

type AgentConfig struct {
	ID             engine.AgentID `yaml:"id"`
	Strategy       string         `yaml:"strategy"`
	Cash           int64          `yaml:"cash"`
	FastTick       time.Duration  `yaml:"fast_tick"`
	SlowTick       time.Duration  `yaml:"slow_tick"`
	DefaultPolicy  *DefaultPolicy `yaml:"default_policy"`
	StrategyParams StrategyParams `yaml:"strategy_params"`
}

type DefaultPolicy struct {
	Participation bool    `yaml:"participation"`
	Bias          float64 `yaml:"bias"`
	RiskAppetite  float64 `yaml:"risk_appetite"`
	Aggression    float64 `yaml:"aggression"`
}

type StrategyParams struct {
	// shared
	Size int64 `yaml:"size"`

	// noise
	Rate      float64 `yaml:"rate"`
	SeedPrice int64   `yaml:"seed_price"`
	Band      int64   `yaml:"band"`

	// market maker
	HalfSpread  int64   `yaml:"half_spread"`
	MaxPosition int64   `yaml:"max_position"`
	SkewPerLot  float64 `yaml:"skew_per_lot"`
}

// --- load yaml to fleet ---

// LoadFleet reads a YAML fleet configuration file, validating agents and applying defaults to generate a Fleet struct.
func LoadFleet(path string) (*Fleet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading fleet config: %w", err)
	}

	var fleet Fleet
	if err := yaml.Unmarshal(data, &fleet); err != nil {
		return nil, fmt.Errorf("parsing fleet config: %w", err)
	}

	if len(fleet.Agents) == 0 {
		return nil, fmt.Errorf("fleet config has no agents")
	}

	seen := make(map[engine.AgentID]struct{}, len(fleet.Agents))

	for i := range fleet.Agents { // iterate through indices because configs are mutated
		agent := &fleet.Agents[i]

		// verify agent ID is non-zero
		if agent.ID == 0 {
			return nil, fmt.Errorf("agent %d: id cannot be zero", i)
		}

		// verify agent ID is unique
		if _, ok := seen[agent.ID]; ok {
			return nil, fmt.Errorf("duplicate agent id %d", agent.ID)
		}
		seen[agent.ID] = struct{}{}

		// apply defaults where config doesn't override
		if agent.Cash == 0 {
			agent.Cash = fleet.Defaults.Cash
		}
		if agent.FastTick == 0 {
			agent.FastTick = fleet.Defaults.FastTick
		}
		if agent.SlowTick == 0 {
			agent.SlowTick = fleet.Defaults.SlowTick
		}
		if agent.DefaultPolicy == nil {
			policy := fleet.Defaults.Policy // make a copy so every agent doesn't point to the same policy
			agent.DefaultPolicy = &policy
		}
	}

	return &fleet, nil
}
