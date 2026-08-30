package engine

import (
	"context"
	"fmt"
	"sync"
)

// Exchange is a collection of engines mapped by symbol.
type Exchange struct {
	engines map[string]*Engine
	done    chan struct{} // empty channel; closed when all engines belonging to the exchange have drained and stopped
}

// NewExchange initializes an exchange with a new engine for each symbol.
// Engines share an exchange-wide events output channel and cancel function, but each owns its assigned JournalWriter.
func NewExchange(symbols []string, buf int, events chan<- []Event, journals map[string]JournalWriter, cancel context.CancelFunc) *Exchange {
	engines := make(map[string]*Engine, len(symbols))

	for _, symbol := range symbols {
		engines[symbol] = NewEngine(symbol, buf, events, journals[symbol], cancel)
	}

	return &Exchange{engines: engines, done: make(chan struct{})}
}

// Restore applies commands synchronously to the book to rebuild state from a journal.
// It must be called before Run(), as concurrent use with the engine running will result in a data race.
func (x *Exchange) Restore(symbol string, cmds []Command) error {
	e, ok := x.engines[symbol]
	if !ok {
		return fmt.Errorf("unknown symbol %q", symbol)
	}

	e.Restore(cmds)

	return nil
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

// TryDispatch attempts a non-blocking send to the engine registered to the given symbol.
// It returns accepted set to false with no error if the engine's queue is full, or an error if the symbol is unknown.
func (x *Exchange) TryDispatch(symbol string, cmd Command) (accepted bool, err error) {
	e, ok := x.engines[symbol]
	if !ok {
		return false, fmt.Errorf("unknown symbol %q", symbol)
	}

	select {
	case e.Cmds() <- cmd:
		return true, nil
	default:
		return false, nil // queue is full
	}
}

// Done is closed when the exchange has finished shutting down; wait on this before closing the events channel.
func (x *Exchange) Done() <-chan struct{} {
	return x.done
}

// Depth returns the top-n bids and asks in the book as an independent snapshot, or an error if symbol doesn't exist.
// Results are never overwritten after being returned; safe to retain.
func (x *Exchange) Depth(symbol string, n int) (bids, asks []PriceLevel, err error) {
	e, ok := x.engines[symbol]
	if !ok {
		return nil, nil, fmt.Errorf("unknown symbol %q", symbol)
	}

	bids, asks = e.Depth(n)

	return bids, asks, nil
}
