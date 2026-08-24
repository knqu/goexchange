package engine

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

// --- helpers ---

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

// --- throughput benchmarks ---

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

// --- latency benchmarks ---

// TestLatencyDistribution measures the latency distribution of the the full engine loop, including p50, p99, and p99.9.
func TestLatencyDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("latency measurement is slow")
	}

	const n = 1_000_000

	events := make(chan []Event, 4096)
	engine := NewEngine("ACME", 4096, events)
	var engineDone sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	engineDone.Add(1)
	go func() {
		defer engineDone.Done()
		engine.Run(ctx)
	}()

	sendTimes := make([]time.Time, n+1)      // recorded by sender; times are indexed by OrderID
	latencies := make([]time.Duration, 0, n) // computed by drainer

	// drain batches of events from the channel, recording each processed command's latency
	drained := make(chan struct{})
	go func() {
		for batch := range events {
			now := time.Now() // batch arrival time = completion time (batch is atomic)
			for _, event := range batch {
				if event.Type == EventAccepted {
					latencies = append(latencies, now.Sub(sendTimes[event.OrderID]))
				}
			}
		}
		close(drained)
	}()

	cmds := buildAlternatingCommands(n)

	// send all commands to the engine, recording each operation's send time
	for i := 0; i < n; i++ {
		sendTimes[cmds[i].Order.ID] = time.Now()
		engine.Cmds() <- cmds[i]
	}

	cancel()
	engineDone.Wait()
	close(events)
	<-drained // wait until drainer finishes before reading latencies

	size := len(latencies)
	if size == 0 {
		t.Fatal("no latencies recorded")
	}

	slices.Sort(latencies)
	p50 := latencies[size*50/100]
	p99 := latencies[size*99/100]
	p999 := latencies[size*999/1000]
	max := latencies[size-1]

	t.Logf("p50=%v p99=%v p99.9=%v max=%v", p50, p99, p999, max)
}
