package feed

import (
	"encoding/json"
	"log"
	"slices"

	"github.com/knqu/goexchange/internal/broadcast"
	"github.com/knqu/goexchange/internal/engine"
)

// --- utility data structures ---

// Snapshot captures the depth of the entire book at a single point in time, with bids and asks ordered best-to-worst.
// This is used by the consumer to establish a baseline, after which deltas are applied to stay up-to-date.
type Snapshot struct {
	Seq  uint64 // seq of last emitted delta
	Bids []engine.PriceLevel
	Asks []engine.PriceLevel
}

func (s Snapshot) MarshalJSON() ([]byte, error) {
	type alias Snapshot // create new type with same fields but no methods (avoid infinite recursion)
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{Type: "snapshot", alias: alias(s)})
}

// Delta is a single incremental book update, stamped with a monotonically increasing seq (to detect dropped messages).
type Delta struct {
	Seq      uint64
	Side     engine.Side
	Price    int64
	Quantity int64
}

func (d Delta) MarshalJSON() ([]byte, error) {
	type alias Delta // create new type with same fields but no methods (avoid infinite recursion)
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{Type: "delta", alias: alias(d)})
}

// Trade represents a single confirmed trade, signaled by an EventTraded.
type Trade struct {
	MakerOrderID engine.OrderID
	TakerOrderID engine.OrderID
	Side         engine.Side
	Price        int64
	Quantity     int64
}

func (t Trade) MarshalJSON() ([]byte, error) {
	type alias Trade
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{Type: "trade", alias: alias(t)})
}

// subReq represents a request to subscribe to the aggregator's updates.
// Subscription requests must be processed in sequence with events because snapshotting reads live maps (bids/asks).
type subReq struct {
	buf   int
	reply chan *broadcast.Subscriber
}

// --- aggregator data structures and methods ---

// Aggregator is a mirror of a single symbol's book, mapping the price of each level to its total resting quantity.
// It is derived from engine events (directly examining the book would break its single-goroutine invariant).
// All aggregator methods must be called from the same goroutine; it is not safe for concurrent use.
type Aggregator struct {
	bids    map[int64]int64
	asks    map[int64]int64
	hub     *broadcast.Hub
	subReqs chan *subReq
	seq     uint64
}

func (a *Aggregator) nextSeq() uint64 {
	a.seq++
	return a.seq
}

// NewAggregator initializes a new aggregator with its own broadcast hub for publishing updates.
func NewAggregator() *Aggregator {
	return &Aggregator{
		bids:    make(map[int64]int64),
		asks:    make(map[int64]int64),
		hub:     broadcast.NewHub(),
		subReqs: make(chan *subReq),
	}
}

// Run starts the aggregator's main loop, processing engine events and handling subscription requests.
func (a *Aggregator) Run(events <-chan []engine.Event) {
	for {
		select {
		case batch, ok := <-events:
			// aggregator returns once events channel is closed; no need to select on <-ctx.Done()
			if !ok {
				return
			}
			for _, event := range batch {
				a.consume(event)
			}
		case req := <-a.subReqs:
			sub := a.hub.Subscribe(req.buf)
			a.sendSnapshot(sub.Messages) // sending snapshot before caller gets sub guarantees snapshot will beat delta
			req.reply <- sub
		}
	}
}

// Subscribe registers a new subscriber and returns a struct containing their messages channel; buf must be >= 1.
func (a *Aggregator) Subscribe(buf int) *broadcast.Subscriber {
	if buf < 1 {
		buf = 1 // clamp buffer size to prevent sendSnapshot() from blocking and deadlocking Run()
	}

	subReq := &subReq{buf: buf, reply: make(chan *broadcast.Subscriber, 1)}
	a.subReqs <- subReq

	return <-subReq.reply // block caller until subReq has been processed by Run() and subscriber instance is ready
}

// Unsubscribe removes a subscriber and closes their messages channel.
func (a *Aggregator) Unsubscribe(sub *broadcast.Subscriber) {
	a.hub.Unsubscribe(sub)
}

// --- event processing helpers ---

// consume processes an engine event, updating the aggregator's state and publishing a new delta.
func (a *Aggregator) consume(event engine.Event) {
	switch event.Type {
	case engine.EventRested:
		a.update(a.mapFor(event.Side), event.Side, event.Price, event.Quantity)
	case engine.EventTraded:
		// only subtract from maker's quantity (taker's order hasn't rested yet)
		a.update(a.mapFor(event.Side.Other()), event.Side.Other(), event.Price, -event.Quantity)
		a.publishTrade(event.OrderID, event.MakerOrderID, event.Side, event.Price, event.Quantity)
	case engine.EventCanceled:
		a.update(a.mapFor(event.Side), event.Side, event.Price, -event.Quantity)
	}
}

// update adjusts the total quantity for a given price and side and publishes a new delta.
func (a *Aggregator) update(m map[int64]int64, side engine.Side, price, quantityChange int64) {
	total := m[price] + quantityChange

	if total > 0 {
		m[price] = total
	} else {
		delete(m, price)
	}

	a.publishDelta(side, price, total)
}

// mapFor returns the order book map (bids or asks) corresponding to the given side.
func (a *Aggregator) mapFor(side engine.Side) map[int64]int64 {
	if side == engine.Buy {
		return a.bids
	}
	return a.asks
}

// --- data transmission helpers ---

// sendSnapshot sends a full order book snapshot to a specific subscriber's messages channel.
func (a *Aggregator) sendSnapshot(messages chan<- []byte) {
	bids := make([]engine.PriceLevel, 0, len(a.bids))
	for price, quantity := range a.bids {
		bids = append(bids, engine.PriceLevel{Price: price, Quantity: quantity})
	}

	asks := make([]engine.PriceLevel, 0, len(a.asks))
	for price, quantity := range a.asks {
		asks = append(asks, engine.PriceLevel{Price: price, Quantity: quantity})
	}

	slices.SortFunc(bids, func(x, y engine.PriceLevel) int { return int(y.Price - x.Price) }) // high to low
	slices.SortFunc(asks, func(x, y engine.PriceLevel) int { return int(x.Price - y.Price) }) // low to high

	b, err := json.Marshal(Snapshot{
		Seq:  a.seq,
		Bids: bids,
		Asks: asks,
	})
	if err != nil {
		log.Printf("feed: marshal failed: %v", err)
		return
	}

	messages <- b
}

// publishDelta publishes a delta update to all subscribers.
func (a *Aggregator) publishDelta(side engine.Side, price, newQuantity int64) {
	b, err := json.Marshal(Delta{
		Seq:      a.nextSeq(),
		Side:     side,
		Price:    price,
		Quantity: newQuantity,
	})
	if err != nil {
		log.Printf("feed: marshal failed: %v", err)
		return
	}

	a.hub.Publish(b)
}

// publishTrade publishes a confirmed trade to all subscribers.
func (a *Aggregator) publishTrade(takerOrderID, makerOrderID engine.OrderID, side engine.Side, price, quantity int64) {
	b, err := json.Marshal(Trade{
		TakerOrderID: takerOrderID,
		MakerOrderID: makerOrderID,
		Side:         side,
		Price:        price,
		Quantity:     quantity,
	})
	if err != nil {
		log.Printf("feed: marshal failed: %v", err)
		return
	}

	a.hub.Publish(b)
}
