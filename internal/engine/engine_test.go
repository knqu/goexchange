package engine

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrentSubmitters(t *testing.T) {
	events := make(chan []Event, 4096)
	engine := NewEngine("ACME", 1024, events)
	ctx, cancel := context.WithCancel(context.Background())
	go engine.Run(ctx)

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
	var wg sync.WaitGroup // primitive to wait for n goroutines to finish
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				id := OrderID(w*1_000_000 + i) // guarantee uniqueness across submitters
				side := Side(i % 2)
				engine.Cmds() <- Command{Type: CmdSubmit, Order: Order{
					ID: id, AgentID: AgentID(w + 1),
					Side: side, Type: Limit, TIF: Day,
					Price: int64(9900 + i%20), Quantity: 10,
				}}
			}
		}(w)
	}
	wg.Wait()       // block main goroutine until all submitters are done sending
	cancel()        // stop engine loop
	<-engine.Done() // wait until engine's Run has actually returned (to prevent send on closed channel panic)
	close(events)   // ends drainer goroutine's range over events
	<-drained       // main blocks until drainer closes done; otherwise got may be read while drainer is still writing

	if got == 0 {
		t.Fatal("expected events, got none")
	}
}

// TestGracefulShutdownDrainsPendingCommands verifies that when the context is cancelled
// with commands still buffered in cmds, Run processes all of them before exiting.
func TestGracefulShutdownDrainsPendingCommands(t *testing.T) {
	const n = 500 // number of CmdSubmits

	events := make(chan []Event, 4096)
	engine := NewEngine("ACME", n, events) // cmds buffer size must be >= n: all sends must land without blocking
	ctx, cancel := context.WithCancel(context.Background())

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

	// fill cmds buffer without starting Run
	// because the buffer holds n, all n sends complete and sit in the channel unprocessed
	for i := 0; i < n; i++ {
		engine.Cmds() <- Command{Type: CmdSubmit, Order: Order{
			ID: OrderID(i + 1), AgentID: 1,
			Side: Side(i % 2), Type: Limit, TIF: Day,
			Price: int64(9900 + i%20), Quantity: 10,
		}}
	}

	// start Run and immediately cancel; a backlog of n commands is already waiting
	go engine.Run(ctx)
	cancel()

	<-engine.Done() // wait for Run to fully exit (drain complete)
	close(events)
	<-drained

	// every valid CmdSubmit should have emitted exactly one EventAccepted
	if accepted != n {
		t.Fatalf("drained %d of %d commands; graceful shutdown dropped %d",
			accepted, n, n-accepted)
	}
}
