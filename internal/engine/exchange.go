package engine

import (
	"context"
	"fmt"
	"sync"
)

// Exchange is a collection of Engines mapped by symbol.
type Exchange struct {
	engines map[string]*Engine
	done    chan struct{} // empty channel; closed when all engines belonging to the exchange have drained and stopped
}

// NewExchange initializes an Exchange with a new Engine for each symbol and a shared events channel.
func NewExchange(symbols []string, buf int, events chan<- []Event) *Exchange {
	engines := make(map[string]*Engine, len(symbols))

	for _, symbol := range symbols {
		engines[symbol] = NewEngine(symbol, buf, events)
	}

	return &Exchange{engines: engines, done: make(chan struct{})}
}

// Run spawns one goroutine per symbol to run each Engine, and returns immediately.
func (x *Exchange) Run(ctx context.Context) {
	var wg sync.WaitGroup

	// spawn one goroutine per symbol
	for _, e := range x.engines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.Run(ctx)
		}()
	}

	// spawn a goroutine to wait until all engines are done before closing the exchange's done channel
	// we cannot use WaitGroup on the exchange itself because Exchange.Run() is non-blocking, unlike Engine.Run()
	go func() {
		wg.Wait()
		close(x.done)
	}()
}

// Dispatch sends a command to the engine registered to the given symbol, returning an error if the symbol is unknown.
func (x *Exchange) Dispatch(symbol string, cmd Command) error {
	e, ok := x.engines[symbol]
	if !ok {
		return fmt.Errorf("unknown symbol %q", symbol)
	}

	e.Cmds() <- cmd

	return nil
}

// Done is closed when the exchange has finished shutting down; wait on this before closing the events channel.
func (x *Exchange) Done() <-chan struct{} {
	return x.done
}
