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
	done := make(chan struct{}) // empty-signal channel
	go func() {
		for batch := range events {
			got += int64(len(batch))
		}
		close(done)
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
	<-done          // main blocks until drainer closes done; otherwise got may be read while drainer is still writing

	if got == 0 {
		t.Fatal("expected events, got none")
	}
}
