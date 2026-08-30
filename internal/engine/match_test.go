package engine

import (
	"math/rand"
	"slices"
	"testing"
)

// --- helpers ---

// limit constructs a valid limit day order ready to rest.
func limit(id OrderID, agent AgentID, s Side, price, quantity int64) Order {
	return Order{ID: id, AgentID: agent,
		Side: s, Type: Limit, TIF: Day,
		Price: price, Quantity: quantity}
}

// market constructs a valid market order.
func market(id OrderID, agent AgentID, s Side, quantity int64) Order {
	return Order{ID: id, AgentID: agent,
		Side: s, Type: Market,
		Quantity: quantity}
}

// submit runs one order through Book.Apply() and fails the test if the book's invariants break afterward.
func submit(t *testing.T, b *Book, seq *seqCounter, o Order) []Event {
	t.Helper()

	events := b.Apply(Command{Type: CmdSubmit, Order: o}, seq)
	if err := b.audit(); err != nil {
		t.Fatalf("book invariant broken after order %d: %v", o.ID, err)
	}

	return events
}

// --- order matching tests ---

// TestMatching runs a series of matching scenarios, checking that the event stream is exactly as expected.
func TestMatching(t *testing.T) {
	cases := []struct {
		name  string
		setup []Order // rested via Book.Apply()
		order Order   // order under test
		want  []Event // complete expected event slice for order only
	}{
		{
			name:  "price_improvement_fills_at_maker_price",
			setup: []Order{limit(1, 1, Sell, 9955, 100)},
			order: limit(2, 2, Buy, 9960, 100),
			want: []Event{
				// start at Seq = 2 because Seq = 1 was consumed by setup's EventAccepted
				{Type: EventAccepted, Seq: 2, OrderID: 2, Side: Buy, Price: 9960, Quantity: 100},
				{Type: EventTraded, Seq: 3, OrderID: 2, MakerOrderID: 1, Side: Buy, Price: 9955, Quantity: 100},
			},
		},
		{
			name: "sweep_through_level_fills_fifo",
			setup: []Order{
				limit(1, 1, Sell, 9950, 30),
				limit(2, 2, Sell, 9950, 30),
				limit(3, 3, Sell, 9950, 30),
			},
			order: limit(4, 4, Buy, 9950, 100),
			want: []Event{
				{Type: EventAccepted, Seq: 4, OrderID: 4, Side: Buy, Price: 9950, Quantity: 100},
				{Type: EventTraded, Seq: 5, OrderID: 4, MakerOrderID: 1, Side: Buy, Price: 9950, Quantity: 30},
				{Type: EventTraded, Seq: 6, OrderID: 4, MakerOrderID: 2, Side: Buy, Price: 9950, Quantity: 30},
				{Type: EventTraded, Seq: 7, OrderID: 4, MakerOrderID: 3, Side: Buy, Price: 9950, Quantity: 30},
				// Remaining = 10 (no more events; resting is silent)
			},
		},
		{
			name:  "ioc_remainder_expires",
			setup: []Order{limit(1, 1, Sell, 9950, 40)},
			order: Order{ID: 2, AgentID: 2, Side: Buy, Type: Limit, TIF: IOC, Price: 9950, Quantity: 100},
			want: []Event{
				{Type: EventAccepted, Seq: 2, OrderID: 2, Side: Buy, Price: 9950, Quantity: 100},
				{Type: EventTraded, Seq: 3, OrderID: 2, MakerOrderID: 1, Side: Buy, Price: 9950, Quantity: 40},
				{Type: EventExpired, Seq: 4, OrderID: 2, Side: Buy, Price: 9950, Quantity: 60},
			},
		},
		{
			name: "fok_rejects_on_multi_order_level_shortfall",
			setup: []Order{
				limit(1, 1, Sell, 9950, 100),
				limit(2, 2, Sell, 9950, 100),
				limit(3, 3, Sell, 9950, 100),
			},
			order: Order{ID: 4, AgentID: 4, Side: Buy, Type: Limit, TIF: FOK, Price: 9950, Quantity: 400},
			want: []Event{
				{Type: EventRejected, Seq: 4, OrderID: 4, RejectReason: RejectFOKInsufficient},
			},
		},
		{
			name: "self_trade_cancels_own_maker_and_fills_behind",
			setup: []Order{
				limit(1, 1, Sell, 9950, 100), // agent 1's own ask, front of queue
				limit(2, 2, Sell, 9950, 100), // agent 2 behind it
			},
			order: limit(3, 1, Buy, 9950, 100), // agent 1 crosses its own quote
			want: []Event{
				{Type: EventAccepted, Seq: 3, OrderID: 3, Side: Buy, Price: 9950, Quantity: 100},
				{Type: EventCanceled, Seq: 4, OrderID: 1, Side: Sell, Price: 9950, Quantity: 100, CancelReason: CancelSelfTrade},
				{Type: EventTraded, Seq: 5, OrderID: 3, MakerOrderID: 2, Side: Buy, Price: 9950, Quantity: 100},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()
			var seq seqCounter

			for _, o := range tc.setup {
				submit(t, b, &seq, o)
			}

			got := submit(t, b, &seq, tc.order)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("events:\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// TestFOKRejectionLeavesBookUntouched verifies that a rejected FOK order does not mutate the book's state.
func TestFOKRejectionLeavesBookUntouched(t *testing.T) {
	b := NewBook()
	var seq seqCounter

	submit(t, b, &seq, limit(1, 1, Sell, 9950, 100))

	beforeAsks := b.Depth(10).Asks
	submit(t, b, &seq, Order{ID: 2, AgentID: 2, Side: Buy, Type: Limit, TIF: FOK, Price: 9950, Quantity: 500})
	afterAsks := b.Depth(10).Asks

	if !slices.Equal(beforeAsks, afterAsks) {
		t.Fatalf("FOK rejection mutated the book: %+v -> %+v", beforeAsks, afterAsks)
	}
}

// TestBookInvariantsUnderRandomCommands runs a sequence of random commands, checking the book's invariants after each.
func TestBookInvariantsUnderRandomCommands(t *testing.T) {
	const nCommands = 10_000
	rng := rand.New(rand.NewSource(25)) // fixed seed guarantees reproducibility

	b := NewBook()

	var resting []OrderID        // canceling an already-gone ID is fine (just results in a rejection)
	agents := []AgentID{1, 2, 3} // small pool to increase the likelihood of self-matching

	nextID := OrderID(0)
	var seq seqCounter
	var lastSeq uint64

	for i := 0; i < nCommands; i++ {
		var cmd Command

		if len(resting) > 0 && rng.Intn(10) == 0 { // ~10% cancels
			cmd = Command{Type: CmdCancel, CancelID: resting[rng.Intn(len(resting))]} // randomly pick a resting order to cancel
		} else {
			nextID++

			o := Order{
				ID:       nextID,
				AgentID:  agents[rng.Intn(len(agents))],
				Side:     Side(rng.Intn(2)),
				Type:     Limit,
				TIF:      TIF(rng.Intn(3)),
				Price:    int64(9900 + rng.Intn(21)),
				Quantity: int64(1 + rng.Intn(200)),
			}

			if rng.Intn(10) == 0 { // ~10% market orders
				o.Type = Market
				o.TIF = Day
				o.Price = 0
			}
			if o.Type == Limit && o.TIF == Day {
				resting = append(resting, o.ID)
			}

			cmd = Command{Type: CmdSubmit, Order: o}
		}

		events := b.Apply(cmd, &seq)

		// verify audit() invariants hold
		if err := b.audit(); err != nil {
			t.Fatalf("command %d broke the book: %v", i, err)
		}

		// invariant: event stream is gapless (no Seq lost)
		for _, event := range events {
			if event.Seq != lastSeq+1 {
				t.Fatalf("command %d: event seq gap: %d after %d", i, event.Seq, lastSeq)
			}
			lastSeq = event.Seq
		}
	}
}
