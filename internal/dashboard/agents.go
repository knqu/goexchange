package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/knqu/goexchange/internal/agents"
)

// FleetAgent represents an agent along with static details not carried by its observations.
type FleetAgent struct {
	Agent        *agents.Agent
	Strategy     string
	StartingCash int64
}

// agentView is the JSON representation of a single agent served to the dashboard.
type agentView struct {
	Observation  *agents.Observation `json:"observation"` // nil until the agent publishes its first observation
	Strategy     string              `json:"strategy"`
	StartingCash int64               `json:"starting_cash"`
}

// NewAgentsHandler serves the latest observation of each agent given a complete fleet.
func NewAgentsHandler(session string, fleet []FleetAgent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		views := make([]agentView, 0, len(fleet))

		for _, member := range fleet {
			views = append(views, agentView{
				Observation:  member.Agent.Observe(),
				Strategy:     member.Strategy,
				StartingCash: member.StartingCash,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{"session": session, "agents": views})
	})
}
