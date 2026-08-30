package engine

import (
	"fmt"
	"slices"
	"testing"
)

// --- auditors: check structural invariants ---

// audit returns nil if healthy or the first error encountered.
func (bs *bookSide) audit() error {
	side := bs.String()

	for i, lvl := range bs.levels {
		// invariant: no empty levels
		if lvl.orders.Len() == 0 {
			return fmt.Errorf("%s: empty level at price %d not reaped", side, lvl.price)
		}

		// invariant: best-first sort
		if i > 0 && !bs.better(bs.levels[i-1].price, lvl.price) {
			return fmt.Errorf("%s: levels out of order at index %d: %d then %d",
				side, i, bs.levels[i-1].price, lvl.price)
		}

		// invariant: cached bookSide.byPrice agrees with bookSide.levels
		if bs.byPrice[lvl.price] != lvl {
			return fmt.Errorf("%s: byPrice[%d] does not point at the level in the slice", side, lvl.price)
		}

		// invariant: cached level.volume agrees with the sum of Remaining across level.orders
		var sum int64
		for node := lvl.orders.Front(); node != nil; node = node.Next() {
			o := node.Value.(*Order)
			if o.Price != lvl.price {
				return fmt.Errorf("%s: order %d (price %d) resting at level %d", side, o.ID, o.Price, lvl.price)
			}
			sum += o.Remaining
		}
		if lvl.volume != sum {
			return fmt.Errorf("%s level %d: cached volume %d != summed remaining %d",
				side, lvl.price, lvl.volume, sum)
		}
	}

	// invariant: bookSide.byPrice and bookSide.levels agree on count
	if len(bs.byPrice) != len(bs.levels) {
		return fmt.Errorf("%s: byPrice has %d entries, slice has %d levels", side, len(bs.byPrice), len(bs.levels))
	}

	return nil
}

// audit returns nil if healthy or the first error encountered.
func (b *Book) audit() error {
	if err := b.bids.audit(); err != nil {
		return err
	}
	if err := b.asks.audit(); err != nil {
		return err
	}

	// invariant: resting book is not crossed
	if bid, ok := b.BestBid(); ok {
		if ask, ok := b.BestAsk(); ok && bid >= ask {
			return fmt.Errorf("book is crossed: bid %d >= ask %d", bid, ask)
		}
	}

	// invariant: every restingRef in Book.byID resides where it claims
	for id, ref := range b.byID {
		if ref.elem.Value.(*Order) != ref.order {
			return fmt.Errorf("byID[%d]: elem does not hold the ref's order", id)
		}
		if ref.level != ref.side.byPrice[ref.order.Price] {
			return fmt.Errorf("byID[%d]: ref.level is not the live level at price %d", id, ref.order.Price)
		}
	}

	return nil
}

// --- helpers ---

// restOrder constructs an order ready to be rested in the book.
func restOrder(id OrderID, s Side, price, quantity, remaining int64) Order {
	return Order{ID: id, AgentID: 0, // AgentID can be 0 because structural tests bypass match-time validation
		Side: s, Type: Limit, TIF: Day,
		Price: price, Quantity: quantity, Remaining: remaining}
}

// restAll rests every order passed in, auditing after each one.
func restAll(t *testing.T, b *Book, orders []Order) {
	t.Helper()

	for i := range orders { // don't iterate over values, which creates a copy
		b.rest(&orders[i])
		if err := b.audit(); err != nil {
			t.Fatalf("invariants broken after resting order %d: %v", orders[i].ID, err)
		}
	}
}

// --- resting tests: check structural placement ---

// TestRestEnforcesTimePriority verifies that within a level, orders are placed in FIFO order.
func TestRestEnforcesTimePriority(t *testing.T) {
	cases := []struct {
		name         string
		orders       []Order
		checkSide    Side
		wantOrderIDs []OrderID
	}{
		{
			name: "time_priority_within_bid_levels_is_enforced",
			orders: []Order{
				restOrder(2, Buy, 9950, 100, 100),
				restOrder(1, Buy, 9950, 100, 100),
				restOrder(3, Buy, 9950, 100, 100),
			},
			checkSide:    Buy,
			wantOrderIDs: []OrderID{2, 1, 3},
		},
		{
			name: "time_priority_within_ask_levels_is_enforced",
			orders: []Order{
				restOrder(2, Sell, 9950, 100, 100),
				restOrder(1, Sell, 9950, 100, 100),
				restOrder(3, Sell, 9950, 100, 100),
			},
			checkSide:    Sell,
			wantOrderIDs: []OrderID{2, 1, 3},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()

			restAll(t, b, tc.orders)

			price := tc.orders[0].Price // price for all orders must be equal
			side := b.sideFor(tc.checkSide)
			level, exists := side.byPrice[price]
			if !exists {
				t.Fatalf("no %s level at %d after resting orders", side.String(), price)
			}

			var gotOrderIDs []OrderID
			for node := level.orders.Front(); node != nil; node = node.Next() {
				gotOrderIDs = append(gotOrderIDs, node.Value.(*Order).ID)
			}
			if !slices.Equal(gotOrderIDs, tc.wantOrderIDs) {
				t.Fatalf("got ids = %#v, want %#v", gotOrderIDs, tc.wantOrderIDs)
			}
		})
	}
}

// TestRestDecorrelatedRemaining rests orders whose Remaining differs from Quantity.
// Guards against the book accumulating the wrong field into level.volume on every rest.
func TestRestDecorrelatedRemaining(t *testing.T) {
	b := NewBook()

	restAll(t, b, []Order{
		restOrder(1, Buy, 9950, 100, 25),
		restOrder(2, Buy, 9950, 100, 50),
	}) // audit() verifies level.volume = 75, not 200
}

// --- cancellation tests: check structural removal ---

// TestRestAndCancel removes orders from the book and verifies that the book's state is consistent afterwards.
func TestRestAndCancel(t *testing.T) {
	cases := []struct {
		name         string
		orders       []Order
		cancel       OrderID
		wantCancelOK bool
		wantBestBid  int64
		wantBidOK    bool
		wantBestAsk  int64
		wantAskOK    bool
	}{
		{
			name: "cancel_only_bid_empties_side",
			orders: []Order{
				restOrder(1, Buy, 9950, 100, 100),
			},
			cancel: 1, wantCancelOK: true,
			wantBestBid: 0, wantBidOK: false,
			wantBestAsk: 0, wantAskOK: false,
		},
		{
			name: "cancel_best_bid_promotes_next_level",
			orders: []Order{
				restOrder(1, Buy, 9950, 100, 100),
				restOrder(2, Buy, 9940, 200, 200),
			},
			cancel: 1, wantCancelOK: true,
			wantBestBid: 9940, wantBidOK: true,
			wantBestAsk: 0, wantAskOK: false,
		},
		{
			name: "cancel_best_ask_promotes_next_level",
			orders: []Order{
				restOrder(1, Sell, 9950, 100, 100),
				restOrder(2, Sell, 9960, 200, 200),
			},
			cancel: 1, wantCancelOK: true,
			wantBestBid: 0, wantBidOK: false,
			wantBestAsk: 9960, wantAskOK: true,
		},
		{
			name: "cancel_unknown_id_is_refused",
			orders: []Order{
				restOrder(1, Buy, 9950, 100, 100),
			},
			cancel: 99, wantCancelOK: false,
			wantBestBid: 9950, wantBidOK: true,
			wantBestAsk: 0, wantAskOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()

			restAll(t, b, tc.orders)

			o, ok := b.cancel(tc.cancel)
			if ok != tc.wantCancelOK {
				t.Fatalf("cancel ok = %v, want %v", ok, tc.wantCancelOK)
			}
			if ok && o.ID != tc.cancel {
				t.Fatalf("cancelled order id = %d, want %d", o.ID, tc.cancel)
			}
			if err := b.audit(); err != nil {
				t.Fatalf("invariants broken after cancel: %v", err)
			}

			gotBestBid, okBid := b.BestBid()
			if okBid != tc.wantBidOK || gotBestBid != tc.wantBestBid {
				t.Fatalf("best bid = %d, %v; want %d, %v", gotBestBid, okBid, tc.wantBestBid, tc.wantBidOK)
			}
			gotBestAsk, okAsk := b.BestAsk()
			if okAsk != tc.wantAskOK || gotBestAsk != tc.wantBestAsk {
				t.Fatalf("best ask = %d, %v; want %d, %v", gotBestAsk, okAsk, tc.wantBestAsk, tc.wantAskOK)
			}
		})
	}
}

// TestCancelMiddleOfQueue cancels the middle order of a three-deep FIFO,
// exercising a linked-list unlink with live neighbors on both sides.
func TestCancelMiddleOfQueue(t *testing.T) {
	b := NewBook()

	restAll(t, b, []Order{
		restOrder(1, Buy, 9950, 100, 100),
		restOrder(2, Buy, 9950, 100, 100),
		restOrder(3, Buy, 9950, 100, 100),
	})
	wantOrderIDs := []OrderID{1, 3}

	if _, ok := b.cancel(2); !ok {
		t.Fatal("cancel of resting middle order failed")
	}
	if err := b.audit(); err != nil {
		t.Fatal(err)
	}

	var gotOrderIDs []OrderID
	for node := b.bids.byPrice[9950].orders.Front(); node != nil; node = node.Next() {
		gotOrderIDs = append(gotOrderIDs, node.Value.(*Order).ID)
	}
	if !slices.Equal(gotOrderIDs, wantOrderIDs) {
		t.Fatalf("queue after middle cancel = %#v, want %#v", gotOrderIDs, wantOrderIDs)
	}
}

// TestCancelPartiallyFilled cancels an order with Remaining < Quantity and asserts that the level's volume drops.
func TestCancelPartiallyFilled(t *testing.T) {
	b := NewBook()

	restAll(t, b, []Order{
		restOrder(1, Buy, 9950, 100, 40),
		restOrder(2, Buy, 9950, 100, 100), // second order keeps the level alive to observe volume
	})

	o, ok := b.cancel(1)
	if !ok {
		t.Fatal("cancel of resting order failed")
	}
	if err := b.audit(); err != nil {
		t.Fatal(err)
	}

	if o.Remaining != 40 {
		t.Fatalf("cancelled order remaining = %d, want 40", o.Remaining)
	}
	if got := b.bids.byPrice[9950].volume; got != 100 {
		t.Fatalf("level volume after cancel = %d, want 100 (140 - 40)", got)
	}
}

// TestLevelRebirth empties a price level via Book.cancel(), reaping it, then rests a new order at the same price.
func TestLevelRebirth(t *testing.T) {
	b := NewBook()

	restAll(t, b, []Order{restOrder(1, Buy, 9950, 100, 100)})

	if _, ok := b.cancel(1); !ok {
		t.Fatal("cancel of resting order failed")
	}
	if err := b.audit(); err != nil {
		t.Fatal(err)
	}

	restAll(t, b, []Order{restOrder(2, Buy, 9950, 50, 50)})

	lvl, exists := b.bids.byPrice[9950]
	if !exists {
		t.Fatal("no bid level at 9950 after rebirth")
	}
	if got := lvl.orders.Front().Value.(*Order).ID; got != OrderID(2) {
		t.Fatalf("reborn level holds order %d, want 2", got)
	}
}

// --- api tests: check data correctness ---

// TestBestBidAsk verifies that Book.BestBid() and Book.BestAsk() return the best prices and handle emptiness correctly.
func TestBestBidAsk(t *testing.T) {
	t.Run("empty_book_reports_no_prices", func(t *testing.T) {
		b := NewBook()

		if _, ok := b.BestBid(); ok {
			t.Fatal("best bid ok = true on empty book")
		}
		if _, ok := b.BestAsk(); ok {
			t.Fatal("best ask ok = true on empty book")
		}
	})

	t.Run("best_prices_from_scrambled_rests", func(t *testing.T) {
		b := NewBook()

		// scrambled arrival means best-price selection must come from better()'s sort, not insertion order
		restAll(t, b, []Order{
			restOrder(1, Buy, 9930, 100, 100),
			restOrder(2, Buy, 9950, 100, 100),
			restOrder(3, Buy, 9940, 100, 100),
			restOrder(4, Sell, 9980, 100, 100),
			restOrder(5, Sell, 9960, 100, 100),
			restOrder(6, Sell, 9970, 100, 100),
		})

		if got, ok := b.BestBid(); !ok || got != 9950 {
			t.Fatalf("best bid = %d, %v; want 9950, true", got, ok)
		}
		if got, ok := b.BestAsk(); !ok || got != 9960 {
			t.Fatalf("best ask = %d, %v; want 9960, true", got, ok)
		}
	})
}

// TestDepth verifies that Book.Depth() returns the correct []PriceLevel holding the best n levels of each side.
func TestDepth(t *testing.T) {
	cases := []struct {
		name     string
		orders   []Order
		n        int
		wantBids []PriceLevel
		wantAsks []PriceLevel
	}{
		{
			name:     "empty_book_returns_empty_slices",
			orders:   nil,
			n:        5,
			wantBids: nil,
			wantAsks: nil,
		},
		{
			name: "n_exceeding_levels_returns_all_levels",
			orders: []Order{
				restOrder(1, Buy, 9950, 100, 100),
				restOrder(2, Sell, 9960, 80, 80),
			},
			n:        10,
			wantBids: []PriceLevel{{Price: 9950, Quantity: 100}},
			wantAsks: []PriceLevel{{Price: 9960, Quantity: 80}},
		},
		{
			name: "n_truncates_to_best_levels",
			orders: []Order{
				restOrder(1, Buy, 9930, 10, 10),
				restOrder(2, Buy, 9950, 30, 30),
				restOrder(3, Buy, 9940, 20, 20),
				restOrder(4, Sell, 9960, 40, 40),
				restOrder(5, Sell, 9970, 50, 50),
				restOrder(6, Sell, 9980, 60, 60),
			},
			n:        2,
			wantBids: []PriceLevel{{Price: 9950, Quantity: 30}, {Price: 9940, Quantity: 20}},
			wantAsks: []PriceLevel{{Price: 9960, Quantity: 40}, {Price: 9970, Quantity: 50}},
		},
		{
			name: "same_price_orders_aggregate_into_one_level",
			orders: []Order{
				restOrder(1, Buy, 9950, 100, 25),
				restOrder(2, Buy, 9950, 100, 50),
			},
			n:        5,
			wantBids: []PriceLevel{{Price: 9950, Quantity: 75}}, // sums Remaining, not Quantity
			wantAsks: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()

			restAll(t, b, tc.orders)

			depth := b.Depth(tc.n)
			gotBids, gotAsks := depth.Bids, depth.Asks

			if !slices.Equal(gotBids, tc.wantBids) {
				t.Errorf("bids = %+v, want %+v", gotBids, tc.wantBids)
			}
			if !slices.Equal(gotAsks, tc.wantAsks) {
				t.Errorf("asks = %+v, want %+v", gotAsks, tc.wantAsks)
			}
		})
	}
}
