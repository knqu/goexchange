package feed

import (
	"encoding/json"
	"testing"

	"github.com/knqu/goexchange/internal/engine"
)

func TestAggregator(t *testing.T) {
	aggregator := NewAggregator()

	aggregator.Seed(engine.DepthSnapshot{Bids: []engine.PriceLevel{{Price: 10000, Quantity: 10}}})

	events := make(chan engine.Event)
	go aggregator.Run(events)
	defer close(events) // stops aggregator once test finishes

	sub := aggregator.Subscribe(8)
	defer aggregator.Unsubscribe(sub)

	var snapshot Snapshot
	if err := json.Unmarshal(<-sub.Messages, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Bids) != 1 || snapshot.Bids[0].Quantity != 10 {
		t.Fatalf("restored book missing: %+v", snapshot)
	}

	events <- engine.Event{Type: engine.EventCanceled, Side: engine.Buy, Price: 10000, Quantity: 4}

	var delta Delta
	if err := json.Unmarshal(<-sub.Messages, &delta); err != nil {
		t.Fatal(err)
	}
	if delta.Quantity != 6 || delta.Seq != snapshot.Seq+1 {
		t.Fatalf("bad post-restore delta: %+v", delta)
	}
}

func TestAggregatorHaltStatus(t *testing.T) {
	aggregator := NewAggregator()

	events := make(chan engine.Event)
	go aggregator.Run(events)
	defer close(events) // stops aggregator once test finishes

	sub := aggregator.Subscribe(8)
	defer aggregator.Unsubscribe(sub)
	<-sub.Messages // initial (unhalted) snapshot

	// existing subscribers are notified when the engine applies a halt
	events <- engine.Event{Type: engine.EventHalted}

	var status Status
	if err := json.Unmarshal(<-sub.Messages, &status); err != nil {
		t.Fatal(err)
	}
	if !status.Halted {
		t.Fatal("halt not published")
	}

	// new subscribers learn the current state from their snapshot
	late := aggregator.Subscribe(8)
	defer aggregator.Unsubscribe(late)

	var snapshot Snapshot
	if err := json.Unmarshal(<-late.Messages, &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.Halted {
		t.Fatal("snapshot missing halt state")
	}
}
