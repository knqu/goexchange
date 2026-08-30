package journal

import (
	"context"
	"math/rand"
	"path/filepath"
	"sync"
	"testing"

	"github.com/knqu/goexchange/internal/engine"
)

// TestReplayIsDeterministic verifies that journal replay into a fresh engine produces byte-identical resting state.
func TestReplayIsDeterministic(t *testing.T) {
	const nCommands = 10_000

	for run := 0; run < 5; run++ {
		path := filepath.Join(t.TempDir(), "test.jnl")

		cmds := buildCommands(rand.New(rand.NewSource(25)), nCommands)

		hashA := runAndJournal(t, path, cmds) // apply command sequence live, journaling each command
		hashB := replayIntoEngine(t, path)    // rebuild a fresh engine from the journal

		if hashA != hashB {
			t.Fatalf("run %d: replay diverged from original (hash %d != %d)", run, hashA, hashB)
		}
	}
}

// runAndJournal feeds cmds through a live engine and journals them, returning the engine's final state hash.
func runAndJournal(t *testing.T, path string, cmds []engine.Command) uint64 {
	t.Helper()

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	events := make(chan []engine.Event, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	eng := engine.NewEngine("ACME", len(cmds), events, writer, cancel)
	var engineDone sync.WaitGroup

	engineDone.Add(1)
	go func() {
		defer engineDone.Done()
		eng.Run(ctx)
	}()

	drained := make(chan struct{})
	go func() {
		for range events {
			// drain events (until closed) so the engine never blocks on its output channel
		}
		close(drained)
	}()

	for _, cmd := range cmds {
		eng.Cmds() <- cmd
	}

	cancel()
	engineDone.Wait()
	close(events)
	<-drained

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return eng.StateHash()
}

// replayIntoEngine builds a fresh engine, replaying the journal to restore it and returning the resulting state hash.
func replayIntoEngine(t *testing.T, path string) uint64 {
	t.Helper()

	events := make(chan []engine.Event, 4096)
	eng := engine.NewEngine("ACME", 4096, events, nil, nil)

	var cmds []engine.Command

	if err := Replay(path, func(cmd engine.Command) error {
		cmds = append(cmds, cmd)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	eng.Restore(cmds)

	return eng.StateHash()
}

// buildCommands generates a deterministic sequence of submits/cancels over a small agent pool and tight price band.
func buildCommands(rng *rand.Rand, n int) []engine.Command {
	cmds := make([]engine.Command, 0, n)

	agents := []engine.AgentID{1, 2, 3}
	var resting []engine.OrderID
	var nextID engine.OrderID

	for i := 0; i < n; i++ {
		if len(resting) > 0 && rng.Intn(10) == 0 { // ~10% cancels
			id := resting[rng.Intn(len(resting))]
			cmds = append(cmds, engine.Command{Type: engine.CmdCancel, CancelID: id})
			continue
		}

		nextID++

		o := engine.Order{
			ID:       nextID,
			AgentID:  agents[rng.Intn(len(agents))],
			Side:     engine.Side(rng.Intn(2)),
			Type:     engine.Limit,
			TIF:      engine.Day,
			Price:    int64(9900 + rng.Intn(21)),
			Quantity: int64(1 + rng.Intn(200)),
		}

		resting = append(resting, o.ID)
		cmds = append(cmds, engine.Command{Type: engine.CmdSubmit, Order: o})
	}

	return cmds
}
