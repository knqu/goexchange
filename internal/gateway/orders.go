package gateway

import (
	"fmt"

	"github.com/knqu/goexchange/internal/engine"
)

// SubmitOrder validates an order, assigns it an ID, and dispatches a CmdSubmit to the exchange.
func (g *Gateway) SubmitOrder(symbol string, order engine.Order) (engine.OrderID, error) {
	if order.AgentID == 0 {
		return 0, fmt.Errorf("%w: agent id must be non-zero", ErrInvalidRequest)
	}
	if order.Type == engine.Limit && order.Price <= 0 {
		return 0, fmt.Errorf("%w: limit order needs positive price, got %d", ErrInvalidRequest, order.Price)
	}
	if order.Type == engine.Market && order.Price != 0 {
		return 0, fmt.Errorf("%w: market order must not carry a price", ErrInvalidRequest)
	}
	if order.Quantity <= 0 {
		return 0, fmt.Errorf("%w: quantity must be positive, got %d", ErrInvalidRequest, order.Quantity)
	}

	order.ID = g.nextOrderID()

	if err := g.dispatch(symbol, engine.Command{
		Type:  engine.CmdSubmit,
		Order: order,
	}); err != nil {
		return 0, err
	}

	return order.ID, nil
}

// CancelOrder dispatches a resting order cancellation to the exchange.
func (g *Gateway) CancelOrder(agentID engine.AgentID, symbol string, orderID engine.OrderID) error {
	if agentID == 0 {
		return fmt.Errorf("%w: agent id must be non-zero", ErrInvalidRequest)
	}
	if orderID == 0 {
		return fmt.Errorf("%w: order id must be non-zero", ErrInvalidRequest)
	}

	return g.dispatch(symbol, engine.Command{
		Type:     engine.CmdCancel,
		CancelID: orderID,
	})
}
