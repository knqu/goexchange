package engine

import (
	"context"
	"sync"
	"testing"
)

// BenchmarkMatching measures the throughput of the pure matching algorithm (no channels or goroutines involved).
func BenchmarkMatching(b *testing.B) {
	book := NewBook()
	var seq seqCounter

	cmds := buildAlternatingCommands(b.N)

	b.ReportAllocs() // report allocations/op alongside ns/op
	b.ResetTimer()   // discard all setup from the measurement

	for i := 0; i < b.N; i++ {
		book.Apply(cmds[i], &seq)
	}

	b.StopTimer()

	opsPerSec := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(opsPerSec, "matches/sec")
}

// BenchmarkEngine measures the full engine loop, including channel, goroutine, and event drainage overhead.
func BenchmarkEngine(b *testing.B) {
	events := make(chan []Event, 4096)
	engine := NewEngine("ACME", 4096, events)
	var engineDone sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	engineDone.Add(1)
	go func() {
		defer engineDone.Done()
		engine.Run(ctx)
	}()

	drained := make(chan struct{})
	go func() {
		for range events {
			// drain events (until closed) so the engine never blocks on its output channel
		}
		close(drained)
	}()

	cmds := buildAlternatingCommands(b.N)

	b.ReportAllocs() // report allocations/op alongside ns/op
	b.ResetTimer()   // discard all setup from the measurement

	for i := 0; i < b.N; i++ {
		engine.Cmds() <- cmds[i]
	}

	cancel()
	engineDone.Wait()

	b.StopTimer() // only stop the timer after the engine has finished processing buffered commands

	close(events)
	<-drained

	opsPerSec := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(opsPerSec, "orders/sec")
}

// buildAlternatingCommands pre-builds a command stream, alternating buy/sell at a single price (so they match).
func buildAlternatingCommands(n int) []Command {
	cmds := make([]Command, n)

	for i := 0; i < n; i++ {
		cmds[i] = Command{Type: CmdSubmit, Order: Order{
			ID: OrderID(i + 1), AgentID: AgentID(i%2 + 1),
			Side: Side(i % 2), Type: Limit, TIF: Day,
			Price: 10000, Quantity: 1,
		}}
	}

	return cmds
}
