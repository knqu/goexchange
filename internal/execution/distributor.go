package execution

import (
	"log"
	"sync"

	"github.com/knqu/goexchange/internal/engine"
)

// Distributor notifies involved agents of completed trades from the engine's event stream.
// Unlike messages emitted by the aggregator, fills are delivered reliably to their intended recipients.
type Distributor struct {
	mu    sync.RWMutex
	fills map[engine.AgentID]chan Fill
}

// NewDistributor initializes a new Distributor with an empty map of agent fills channels.
func NewDistributor() *Distributor {
	return &Distributor{fills: make(map[engine.AgentID]chan Fill)}
}

// Register creates a fills channel for a single agent and returns it as receive-only.
// buf should be generous; a full buffer will result in fills being dropped, corrupting an agent's internal state.
func (d *Distributor) Register(agentID engine.AgentID, buf int) <-chan Fill {
	ch := make(chan Fill, buf)

	d.mu.Lock()
	d.fills[agentID] = ch
	d.mu.Unlock()

	return ch
}

// Unregister removes an agent from the distributor and closes its fills channel.
func (d *Distributor) Unregister(agentID engine.AgentID) {
	d.mu.Lock()
	ch, ok := d.fills[agentID]
	delete(d.fills, agentID)
	d.mu.Unlock()

	if ok {
		close(ch)
	}
}

// GenerateFill notifies both agents involved in a trade (taker and maker) of an executed order.
func (d *Distributor) GenerateFill(symbol string, trade engine.Event) {
	d.send(trade.AgentID, Fill{
		OrderID:  trade.OrderID,
		Symbol:   symbol,
		Side:     trade.Side,
		Price:    trade.Price,
		Quantity: trade.Quantity,
	})

	d.send(trade.MakerAgentID, Fill{
		OrderID:  trade.MakerOrderID,
		Symbol:   symbol,
		Side:     trade.Side.Other(),
		Price:    trade.Price,
		Quantity: trade.Quantity,
	})
}

// send looks up the fills channel associated with the given agentID, and, if it exists, sends the given fill into it.
func (d *Distributor) send(agentID engine.AgentID, fill Fill) {
	d.mu.RLock()
	ch, ok := d.fills[agentID]
	d.mu.RUnlock()

	if !ok {
		return // skip if agent hasn't been registered
	}

	select {
	case ch <- fill:
	default:
		// todo: evict agents who can't keep up (to prevent client-side state corruption)
		log.Printf("execution: dropped fill for agent %d due to full buffer", agentID)
	}
}

// Close closes each agent's fills channel.
func (d *Distributor) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for agentID, ch := range d.fills {
		delete(d.fills, agentID)
		close(ch)
	}
}
