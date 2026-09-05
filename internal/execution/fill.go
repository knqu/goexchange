package execution

import "github.com/knqu/goexchange/internal/engine"

// Fill represents a trade confirmation, notifying an agent that one of its orders was executed.
type Fill struct {
	OrderID  engine.OrderID
	Symbol   string
	Side     engine.Side
	Price    int64
	Quantity int64
}
