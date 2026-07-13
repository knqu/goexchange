package engine

import (
	"container/list"
	"slices"
	"sort"
)

// --- level ---

// level represents all resting orders at a certain price, in order of arrival.
// Enforces time priority within a price.
type level struct {
	price  int64
	orders *list.List
	volume int64 // cached sum of Remaining across Orders
}

func newLevel(price int64) *level {
	return &level{price: price, orders: list.New()}
}

// --- book side ---

// bookSide is one side of the book; bids are sorted descending (highest first), asks ascending (lowest first).
// Enforces price priority.
type bookSide struct {
	side    Side
	levels  []*level         // best (most competitive) level is levels[0]
	byPrice map[int64]*level // cache of levels by price
}

func newBookSide(s Side) *bookSide {
	return &bookSide{side: s, byPrice: make(map[int64]*level)}
}

// better indicates whether price a is "better" (more competitive) than b on side bs.
func (bs *bookSide) better(a, b int64) bool {
	if bs.side == Buy {
		return a > b
	}
	return a < b
}

// findIndex returns the index where a level with the given price is,
// or where it should be inserted to maintain best-first order.
func (bs *bookSide) findIndex(price int64) int {
	return sort.Search(len(bs.levels), func(i int) bool {
		return !bs.better(bs.levels[i].price, price)
	})
}

// getOrCreateLevel returns the level for price, creating and inserting it if needed.
func (bs *bookSide) getOrCreateLevel(price int64) *level {
	if lvl, exists := bs.byPrice[price]; exists {
		return lvl
	}

	lvl := newLevel(price)
	i := bs.findIndex(price)
	bs.levels = slices.Insert(bs.levels, i, lvl)

	bs.byPrice[price] = lvl
	return lvl
}

// removeLevelAt deletes a level guaranteed empty by the caller at index i.
func (bs *bookSide) removeLevelAt(i int) {
	delete(bs.byPrice, bs.levels[i].price)
	bs.levels = slices.Delete(bs.levels, i, i+1)
}

// --- book ---

// restingRef enables finding and removing resting orders in O(1).
type restingRef struct {
	order *Order
	elem  *list.Element // the exact node an order occupies in its level's FIFO
	side  *bookSide
	level *level
}

// Book is a price-time priority limit order book for a single symbol.
type Book struct {
	bids *bookSide
	asks *bookSide
	byID map[OrderID]*restingRef // cache of orders by id
}

// NewBook initializes a Book with bids, asks, and byID ready to use.
func NewBook() *Book {
	return &Book{
		bids: newBookSide(Buy),
		asks: newBookSide(Sell),
		byID: make(map[OrderID]*restingRef),
	}
}

// sideFor returns the bookSide corresponding to the given order Side.
func (b *Book) sideFor(s Side) *bookSide {
	if s == Buy {
		return b.bids
	}
	return b.asks
}

// rest places an order into the book.
// Caller guarantees the order does not cross the opposite side and that o.ID is not already resting.
func (b *Book) rest(o *Order) {
	bs := b.sideFor(o.Side)
	lvl := bs.getOrCreateLevel(o.Price)
	elem := lvl.orders.PushBack(o)
	lvl.volume += o.Remaining
	b.byID[o.ID] = &restingRef{order: o, elem: elem, side: bs, level: lvl}
}

// cancel removes a resting order, returning false if the ID isn't resting
// (already filled, already cancelled, never existed).
func (b *Book) cancel(id OrderID) (*Order, bool) {
	ref, exists := b.byID[id]
	if !exists {
		return nil, false
	}

	ref.level.orders.Remove(ref.elem)
	ref.level.volume -= ref.order.Remaining

	if ref.level.orders.Len() == 0 {
		ref.side.removeLevelAt(ref.side.findIndex(ref.level.price))
	}

	delete(b.byID, id)

	return ref.order, true
}

func (b *Book) BestBid() (int64, bool) {
	if len(b.bids.levels) == 0 {
		return 0, false
	}
	return b.bids.levels[0].price, true
}

func (b *Book) BestAsk() (int64, bool) {
	if len(b.asks.levels) == 0 {
		return 0, false
	}
	return b.asks.levels[0].price, true
}
