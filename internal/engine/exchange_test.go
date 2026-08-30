package engine

import (
	"context"
	"sync"
	"testing"
)

// TestDispatchRoutesToCorrectEngine submits unique orders to two symbols and checks that each landed on the right book.
func TestDispatchRoutesToCorrectEngine(t *testing.T) {
	events := make(chan []Event, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	exchange := NewExchange([]string{"ACME", "MSFT"}, 64, events, nil, cancel)
	exchange.Run(ctx)

	// drain events so engines never block on the shared channel
	drained := make(chan struct{})
	go func() {
		for range events {
		}
		close(drained)
	}()

	if err := exchange.Dispatch("ACME", Command{Type: CmdSubmit, Order: Order{
		ID: 1, AgentID: 1,
		Side: Buy, Type: Limit, TIF: Day,
		Price: 9950, Quantity: 100,
	}}); err != nil {
		t.Fatalf("dispatch to ACME: %v", err)
	}

	if err := exchange.Dispatch("MSFT", Command{Type: CmdSubmit, Order: Order{
		ID: 2, AgentID: 1,
		Side: Buy, Type: Limit, TIF: Day,
		Price: 42000, Quantity: 50,
	}}); err != nil {
		t.Fatalf("dispatch to MSFT: %v", err)
	}

	cancel()
	<-exchange.Done()
	close(events)
	<-drained

	// each order must rest on the book registered to its own symbol
	acme := exchange.engines["ACME"].book
	msft := exchange.engines["MSFT"].book

	if bid, ok := acme.BestBid(); !ok || bid != 9950 {
		t.Errorf("ACME best bid = %d, %v; want 9950, true", bid, ok)
	}
	if bid, ok := msft.BestBid(); !ok || bid != 42000 {
		t.Errorf("MSFT best bid = %d, %v; want 42000, true", bid, ok)
	}

	if _, ok := msft.byID[1]; ok {
		t.Error("ACME's order 1 leaked onto MSFT's book")
	}
	if _, ok := acme.byID[2]; ok {
		t.Error("MSFT's order 2 leaked onto ACME's book")
	}
}

// TestDispatchUnknownSymbol verifies dispatching to a nonexistent symbol errors rather than panicking or dropping.
func TestDispatchUnknownSymbol(t *testing.T) {
	events := make(chan []Event, 16)
	exchange := NewExchange([]string{"ACME"}, 64, events, nil, nil)

	// note: no Exchange.Run() or events drainage logic is needed because dispatch fails before any send

	err := exchange.Dispatch("NOPE", Command{Type: CmdSubmit, Order: Order{
		ID: 1, AgentID: 1,
		Side: Buy, Type: Limit, TIF: Day,
		Price: 100, Quantity: 1,
	}})
	if err == nil {
		t.Fatal("expected error dispatching to unknown symbol, got nil")
	}
}

// TestExchangeShutdownIsClean spawns a multi-symbol exchange under concurrent load, then shuts it down.
// Under -race, it proves N engines sending to one events channel don't race and that Exchange.Done() closes correctly.
func TestExchangeShutdownIsClean(t *testing.T) {
	symbols := []string{"ACME", "MSFT", "GOOG"}

	events := make(chan []Event, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	exchange := NewExchange(symbols, 1024, events, nil, cancel)
	exchange.Run(ctx)

	var got int64
	drained := make(chan struct{})
	go func() {
		for batch := range events {
			got += int64(len(batch))
		}
		close(drained)
	}()

	// concurrent submitters spread across all symbols
	var submitters sync.WaitGroup
	for w := 0; w < len(symbols); w++ {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			symbol := symbols[w]
			for i := 0; i < 500; i++ {
				_ = exchange.Dispatch(symbol, Command{Type: CmdSubmit, Order: Order{
					ID:      OrderID(w*1_000_000 + i + 1),
					AgentID: AgentID(w + 1),
					Side:    Side(i % 2), Type: Limit, TIF: Day,
					Price: int64(9900 + i%20), Quantity: 10,
				}})
			}
		}()
	}

	submitters.Wait() // block until all commands are sent
	cancel()          // ask all engines to stop
	<-exchange.Done() // block until every engine has exited
	close(events)     // safe: all senders provably gone
	<-drained

	if got == 0 {
		t.Fatal("expected events across the exchange, got none")
	}
}

// TestExchangeGracefulDrain verifies commands buffered across multiple engines are all processed before shutdown.
// Buffers are filled before starting the exchange so a backlog provably exists when cancellation fires.
func TestExchangeGracefulDrain(t *testing.T) {
	const perSymbol = 300 // number of CmdSubmits per symbol
	symbols := []string{"ACME", "MSFT"}

	events := make(chan []Event, 8192)
	ctx, cancel := context.WithCancel(context.Background())
	exchange := NewExchange(symbols, perSymbol, events, nil, cancel) // buffer >= perSymbol: all commands land

	var accepted int64
	drained := make(chan struct{})
	go func() {
		for batch := range events {
			for _, ev := range batch {
				if ev.Type == EventAccepted {
					accepted++
				}
			}
		}
		close(drained)
	}()

	// fill both engines' cmds buffer without starting the exchange
	for s, symbol := range symbols {
		for i := 0; i < perSymbol; i++ {
			if err := exchange.Dispatch(symbol, Command{Type: CmdSubmit, Order: Order{
				ID:      OrderID(s*1_000_000 + i + 1),
				AgentID: AgentID(s + 1),
				Side:    Side(i % 2), Type: Limit, TIF: Day,
				Price: int64(9900 + i%20), Quantity: 10,
			}}); err != nil {
				t.Fatalf("dispatch: %v", err)
			}
		}
	}

	// start the exchange with perSymbol commands already waiting in each engine's backlog
	exchange.Run(ctx)

	// immediately cancel
	cancel()
	<-exchange.Done()
	close(events)
	<-drained

	want := int64(len(symbols) * perSymbol)
	if accepted != want {
		t.Fatalf("drained %d of %d commands; graceful shutdown dropped %d", accepted, want, want-accepted)
	}
}
