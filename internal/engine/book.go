package engine

import (
	"container/list"
	"encoding/binary"
	"hash/fnv"
	"slices"
	"sort"
)

// --- level ---

// level represents all resting orders at a certain price, in order of arrival.
// Enforces time priority within a price.
type level struct {
	price  int64
	orders *list.List
	volume int64 // cached sum of Remaining across orders
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

func (bs bookSide) String() string {
	switch bs.side {
	case Buy:
		return "bid"
	case Sell:
		return "ask"
	}
	return ""
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
	if lvl, ok := bs.byPrice[price]; ok {
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

// topN returns the top-n prices and their corresponding quantities (volumes) from one side of the book.
// The returned slice is newly allocated and independent; it should never change (do not convert to a reused buffer).
func (bs *bookSide) topN(n int) []PriceLevel {
	n = max(0, min(n, len(bs.levels))) // clamp to [0, len(bs.levels)]; guards make() capacity and slice bounds

	priceLevels := make([]PriceLevel, 0, n)
	for _, lvl := range bs.levels[:n] {
		priceLevels = append(priceLevels, PriceLevel{Price: lvl.price, Quantity: lvl.volume})
	}

	return priceLevels
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
// Caller guarantees the order does not cross the opposite side and that a same-ID order is not already resting.
func (b *Book) rest(o *Order) {
	bs := b.sideFor(o.Side)
	lvl := bs.getOrCreateLevel(o.Price)
	elem := lvl.orders.PushBack(o)
	lvl.volume += o.Remaining
	b.byID[o.ID] = &restingRef{order: o, elem: elem, side: bs, level: lvl}
}

// cancel removes a resting order, returning false if the ID isn't resting (already filled/cancelled or never existed).
func (b *Book) cancel(id OrderID) (*Order, bool) {
	ref, ok := b.byID[id]
	if !ok {
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

// Depth returns the book's top-n bids and asks as an independent DepthSnapshot.
// Results are never overwritten after being returned, so they are safe to retain.
func (b *Book) Depth(n int) DepthSnapshot {
	return DepthSnapshot{Bids: b.bids.topN(n), Asks: b.asks.topN(n)}
}

// StateHash returns a deterministic hash of the book's resting state.
// Two books with identical resting orders (prices, FIFO order, and remaining quantities) will produce the same hash.
func (b *Book) StateHash() uint64 {
	hash := fnv.New64a()

	var buf [8]byte
	writeUint64 := func(value uint64) {
		binary.BigEndian.PutUint64(buf[:], value)
		hash.Write(buf[:])
	}

	for _, bs := range []*bookSide{b.bids, b.asks} {
		for _, lvl := range bs.levels {
			for node := lvl.orders.Front(); node != nil; node = node.Next() {
				o := node.Value.(*Order)
				writeUint64(uint64(o.ID))
				writeUint64(uint64(o.AgentID))
				// Side is already encoded by position (bids walked first, then asks)
				// Type must be Limit and TIF must be Day if an order is resting in the book
				writeUint64(uint64(o.Price))
				writeUint64(uint64(o.Quantity))
				writeUint64(uint64(o.Remaining))
				// Seq is already encoded by position within level (FIFO)
			}
		}
	}

	return hash.Sum64()
}
