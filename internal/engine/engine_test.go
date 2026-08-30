package engine

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrentSubmitters(t *testing.T) {
	events := make(chan []Event, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	engine := NewEngine("ACME", 1024, events, nil, cancel)
	var engineDone sync.WaitGroup

	engineDone.Add(1)
	go func() {
		defer engineDone.Done()
		engine.Run(ctx)
	}()

	// drain (consume) events concurrently and count them
	var got int64
	drained := make(chan struct{}) // empty-signal channel
	go func() {
		for batch := range events {
			got += int64(len(batch))
		}
		close(drained)
	}()

	// simulate pool of concurrent submitters (agents)
	var submitters sync.WaitGroup
	for w := 0; w < 8; w++ {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			for i := 0; i < 1000; i++ {
				engine.Cmds() <- Command{Type: CmdSubmit, Order: Order{
					ID:      OrderID(w*1_000_000 + i), // guarantee uniqueness across submitters
					AgentID: AgentID(w + 1),
					Side:    Side(i % 2), Type: Limit, TIF: Day,
					Price: int64(9900 + i%20), Quantity: 10,
				}}
			}
		}()
	}

	submitters.Wait() // block until all submitters are done sending
	cancel()          // stop engine loop
	engineDone.Wait() // block until Engine.Run() has actually returned (to prevent send on closed channel panic)
	close(events)     // ends drainer goroutine's range over events
	<-drained         // block until drainer closes done; otherwise got may be read while drainer is still writing

	if got == 0 {
		t.Fatal("expected events, got none")
	}
}

// TestGracefulShutdownDrainsPendingCommands verifies that when the context is cancelled
// with commands still buffered in cmds, Engine.Run() processes all of them before exiting.
func TestGracefulShutdownDrainsPendingCommands(t *testing.T) {
	const n = 500 // number of CmdSubmits

	events := make(chan []Event, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	engine := NewEngine("ACME", n, events, nil, cancel) // buf must be >= n: commands must land without blocking
	var engineDone sync.WaitGroup

	// count accepted orders in the event stream to record how many commands the engine actually handled
	var accepted int
	drained := make(chan struct{})
	go func() {
		for batch := range events {
			for _, event := range batch {
				if event.Type == EventAccepted {
					accepted++
				}
			}
		}
		close(drained)
	}()

	// fill cmds buffer without starting the engine
	// because the buffer holds n, all n sends complete and sit in the channel unprocessed
	for i := 0; i < n; i++ {
		engine.Cmds() <- Command{Type: CmdSubmit, Order: Order{
			ID: OrderID(i + 1), AgentID: 1,
			Side: Side(i % 2), Type: Limit, TIF: Day,
			Price: int64(9900 + i%20), Quantity: 10,
		}}
	}

	// start the engine with a backlog of n commands already waiting
	engineDone.Add(1)
	go func() {
		defer engineDone.Done()
		engine.Run(ctx)
	}()

	// immediately cancel
	cancel()
	engineDone.Wait()
	close(events)
	<-drained

	// every valid CmdSubmit should have emitted exactly one EventAccepted
	if accepted != n {
		t.Fatalf("drained %d of %d commands; graceful shutdown dropped %d",
			accepted, n, n-accepted)
	}
}
