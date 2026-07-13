package engine

import (
	"slices"
	"testing"
)

func TestTimePriority(t *testing.T) {
	cases := []struct {
		name         string
		orders       []Order
		wantOrderIDs []OrderID
	}{
		{
			name: "time_priority_within_levels_is_enforced",
			orders: []Order{
				{ID: 2, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 100},
				{ID: 1, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 100},
				{ID: 3, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 100},
			},
			wantOrderIDs: []OrderID{2, 1, 3},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()

			for i := range tc.orders {
				b.rest(&tc.orders[i])
			}

			price := tc.orders[0].Price // price for all orders must be equal
			level, exists := b.bids.byPrice[price]
			if !exists {
				t.Fatalf("no bid level at %d after resting orders", price)
			}

			var gotOrderIDs []OrderID
			for node := level.orders.Front(); node != nil; node = node.Next() {
				order := node.Value.(*Order)
				gotOrderIDs = append(gotOrderIDs, order.ID)
			}
			if !slices.Equal(gotOrderIDs, tc.wantOrderIDs) {
				t.Fatalf("got ids = %#v, want %#v", gotOrderIDs, tc.wantOrderIDs)
			}
		})
	}
}

func TestVolumeCache(t *testing.T) {
	cases := []struct {
		name   string
		orders []Order
	}{
		{
			name: "cached_volume_is_consistent_with_summed_remaining",
			orders: []Order{
				{ID: 1, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 25},
				{ID: 2, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 50},
				{ID: 3, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 75},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()

			for i := range tc.orders {
				b.rest(&tc.orders[i])
			}

			for _, side := range []*bookSide{b.bids, b.asks} {
				for _, lvl := range side.levels {
					gotVolume := lvl.volume
					wantVolume := int64(0)
					for node := lvl.orders.Front(); node != nil; node = node.Next() {
						wantVolume += node.Value.(*Order).Remaining
					}
					if gotVolume != wantVolume {
						t.Fatalf("level %d: cached volume %d != actual %d", lvl.price, gotVolume, wantVolume)
					}
				}
			}
		})
	}
}

func TestRestAndCancel(t *testing.T) {
	cases := []struct {
		name        string
		orders      []Order
		cancel      OrderID
		wantOK      bool
		wantBestBid int64
	}{
		{
			name: "cancel_only_bid_empties_side",
			orders: []Order{
				{ID: 1, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 100},
			},
			cancel: 1, wantOK: true, wantBestBid: 0,
		},
		{
			name: "cancel_best_bid_promotes_next_level",
			orders: []Order{
				{ID: 1, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 100},
				{ID: 2, Side: Buy, Type: Limit, Price: 9940, Quantity: 200, Remaining: 200},
			},
			cancel: 1, wantOK: true, wantBestBid: 9940,
		},
		{
			name: "cancel_unknown_id_is_refused",
			orders: []Order{
				{ID: 1, Side: Buy, Type: Limit, Price: 9950, Quantity: 100, Remaining: 100},
			},
			cancel: 99, wantOK: false, wantBestBid: 9950,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()

			for i := range tc.orders { // do not use `for _, o := range tc.orders` (creates a copy)
				b.rest(&tc.orders[i])
			}

			_, ok := b.cancel(tc.cancel)
			if ok != tc.wantOK {
				t.Fatalf("cancel ok = %v, want %v", ok, tc.wantOK)
			}

			got := int64(0)
			if len(b.bids.levels) > 0 {
				got, _ = b.BestBid()
			}
			if got != tc.wantBestBid {
				t.Fatalf("best bid = %d, want %d", got, tc.wantBestBid)
			}
		})
	}
}
