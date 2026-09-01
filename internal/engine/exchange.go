package engine

import (
	"context"
	"fmt"
	"sync"
)

// Exchange is a collection of engines mapped by symbol.
type Exchange struct {
	engines map[string]*Engine
	events  map[string]chan []Event
	done    chan struct{} // empty channel; closed when all engines belonging to the exchange have drained and stopped
}

// NewExchange initializes an exchange with a new engine (with its own events output channel) for each symbol.
// Engines own their assigned JournalWriter, but share a cancel function (kill switch for the entire exchange).
// Note that the given buffer size is used to create both the commands input and events output channels.
func NewExchange(symbols []string, buf int, journals map[string]JournalWriter, cancel context.CancelFunc) *Exchange {
	engines := make(map[string]*Engine, len(symbols))
	events := make(map[string]chan []Event, len(symbols))

	for _, symbol := range symbols {
		events[symbol] = make(chan []Event, buf)
		engines[symbol] = NewEngine(symbol, buf, events[symbol], journals[symbol], cancel)
	}

	return &Exchange{engines: engines, events: events, done: make(chan struct{})}
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

	// wait until all engines are done before closing each symbol's events channel and the exchange's done channel
	// cannot use WaitGroup on the exchange because Exchange.Run() is non-blocking (unlike Engine.Run(), which blocks)
	go func() {
		wg.Wait()
		for _, ch := range x.events {
			close(ch)
		}
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

// Events returns a map containing the receive-only events output channel for each symbol in the exchange.
func (x *Exchange) Events() map[string]<-chan []Event {
	chans := make(map[string]<-chan []Event, len(x.events))
	for symbol, ch := range x.events {
		chans[symbol] = ch
	}
	return chans
}

// Done is closed when the exchange has finished shutting down, including closing each symbol's events channel.
func (x *Exchange) Done() <-chan struct{} {
	return x.done
}

// Depth returns the book's top-n bids and asks as an independent DepthSnapshot, or an error if symbol doesn't exist.
// Results are never overwritten after being returned, so they are safe to retain.
func (x *Exchange) Depth(symbol string, n int) (DepthSnapshot, error) {
	e, ok := x.engines[symbol]
	if !ok {
		return DepthSnapshot{}, fmt.Errorf("unknown symbol %q", symbol)
	}

	return e.Depth(n), nil
}
