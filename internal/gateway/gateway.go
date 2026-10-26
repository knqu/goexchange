package gateway

import (
	"errors"
	"sync/atomic"

	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/feed"
)

// --- protocol-agnostic gateway errors ---

var (
	ErrInvalidRequest = errors.New("invalid request")
	ErrUnknownSymbol  = errors.New("unknown symbol")
	ErrQueueFull      = errors.New("engine at capacity, retry")
)

// --- gateway setup and dispatch ---

// Gateway is the exchange's entry point; it validates requests, assigns order IDs, and dispatches commands.
type Gateway struct {
	exchange    *engine.Exchange
	aggregators map[string]*feed.Aggregator
	messagesBuf int
	prevOrderID atomic.Uint64 // needs to be atomic because multiple handlers can run concurrently
}

// nextOrderID returns the next exchange-wide order ID (safe for concurrent use).
func (g *Gateway) nextOrderID() engine.OrderID {
	return engine.OrderID(g.prevOrderID.Add(1))
}

// NewGateway initializes a new gateway that wraps the given exchange, updating its internal OrderID counter.
func NewGateway(exchange *engine.Exchange, aggregators map[string]*feed.Aggregator, messagesBuf int, lastOrderID uint64) *Gateway {
	g := &Gateway{exchange: exchange, aggregators: aggregators, messagesBuf: messagesBuf}
	g.prevOrderID.Store(lastOrderID)
	return g
}

// dispatch queues a command in the specified symbol's engine and translates results to gateway errors.
func (g *Gateway) dispatch(symbol string, cmd engine.Command) error {
	accepted, err := g.exchange.TryDispatch(symbol, cmd)
	if err != nil {
		return ErrUnknownSymbol
	}
	if !accepted {
		return ErrQueueFull
	}
	return nil
}
