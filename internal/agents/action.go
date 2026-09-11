package agents

import "github.com/knqu/goexchange/internal/engine"

// --- data structures ---

// ActionType mirrors the engine's CmdType but is limited to the subset of actions available for agents to take.
type ActionType uint8

const (
	ActionSubmit ActionType = iota
	ActionCancel
)

type Action struct {
	Type      ActionType
	Side      engine.Side
	OrderType engine.OrderType
	TIF       engine.TIF
	Price     int64
	Quantity  int64
	CancelID  engine.OrderID
}

// --- factory functions ---

// NewLimit generates a limit order action, deriving price and TIF from current market prices and policy aggression.
// Caller must check that MarketSnapshot.HasBothSides() is true.
func NewLimit(side engine.Side, quantity int64, market MarketSnapshot, policy Policy) Action {
	spread := market.BestAsk() - market.BestBid()
	offset := int64(policy.Aggression * float64(spread) * 2) // cross book if aggression >= 0.5, rest otherwise

	var price int64
	tif := engine.Day // default to day unless crossing the book (don't rest at an aggressive price)

	switch side {
	case engine.Buy:
		price = market.BestBid() + offset
		if price >= market.BestAsk() {
			tif = engine.IOC
		}
	case engine.Sell:
		price = market.BestAsk() - offset
		if price <= market.BestBid() {
			tif = engine.IOC
		}
	}

	return Action{
		Type:      ActionSubmit,
		Side:      side,
		OrderType: engine.Limit,
		TIF:       tif,
		Price:     price,
		Quantity:  quantity,
	}
}

// NewLimitAt generates a limit order action at the given side, price, and quantity (no derived price or TIF).
func NewLimitAt(side engine.Side, price, quantity int64) Action {
	return Action{
		Type:      ActionSubmit,
		Side:      side,
		OrderType: engine.Limit,
		TIF:       engine.Day,
		Price:     price,
		Quantity:  quantity,
	}
}

// NewMarket generates a market order action, to be executed immediately at the best available price.
func NewMarket(side engine.Side, quantity int64) Action {
	return Action{
		Type:      ActionSubmit,
		Side:      side,
		OrderType: engine.Market,
		Quantity:  quantity,
	}
}

// NewCancel generates a cancel order action for the given order ID.
func NewCancel(id engine.OrderID) Action {
	return Action{Type: ActionCancel, CancelID: id}
}
