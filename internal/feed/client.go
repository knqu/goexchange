package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/coder/websocket"
	"github.com/knqu/goexchange/internal/engine"
)

// --- client data structures ---

// Client subscribes to a symbol's market data feed, maintaining an internal book state of bids and asks.
type Client struct {
	url     string
	mu      sync.RWMutex // guards bids/asks and lastSeq, which are read by callers while written to by Run()
	bids    map[int64]int64
	asks    map[int64]int64
	lastSeq uint64 // seq of last applied delta (used to detect gaps)
	trades  chan Trade
}

// NewClient initializes a new Client instance able to connect to the market data feed at the given URL.
func NewClient(url string, buf int) *Client {
	return &Client{
		url:    url,
		bids:   make(map[int64]int64),
		asks:   make(map[int64]int64),
		trades: make(chan Trade, buf),
	}
}

// --- client connection and read loop ---

// Run connects and processes messages until cancelled; it should be run in a separate goroutine.
func (c *Client) Run(ctx context.Context) {
	for {
		if err := c.connectAndRead(ctx); err != nil {
			if ctx.Err() != nil {
				return // context cancelled
			}
			continue // connection dropped; reconnect
		}
	}
}

// connectAndRead connects to the market data feed, reading and routing messages to the correct handler based on type.
func (c *Client) connectAndRead(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	for {
		_, data, err := conn.Read(ctx) // blocks until message or disconnect
		if err != nil {
			return err // disconnect or context cancel
		}

		var probe struct {
			Type string `json:"type"`
		}

		json.Unmarshal(data, &probe)

		switch probe.Type {
		case "snapshot":
			c.applySnapshot(data)
		case "delta":
			if resnapshot := c.applyDelta(data); resnapshot {
				return fmt.Errorf("new snapshot needed due to seq gap in delta stream")
			}
		case "trade":
			c.sendTrade(data)
		}
	}
}

// --- message apply logic ---

// applySnapshot resets the client's book mirror, replacing it with a new snapshot's state.
func (c *Client) applySnapshot(data []byte) {
	var snapshot Snapshot

	if err := json.Unmarshal(data, &snapshot); err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.bids = make(map[int64]int64)
	for _, lvl := range snapshot.Bids {
		c.bids[lvl.Price] = lvl.Quantity
	}

	c.asks = make(map[int64]int64)
	for _, lvl := range snapshot.Asks {
		c.asks[lvl.Price] = lvl.Quantity
	}

	c.lastSeq = snapshot.Seq // next delta should have seq of snapshot.Seq + 1
}

// applyDelta applies a new delta to the client's book.
// It returns true if a new snapshot is needed due to a sequence gap in the delta stream, false otherwise.
func (c *Client) applyDelta(data []byte) bool {
	var delta Delta

	if err := json.Unmarshal(data, &delta); err != nil {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if delta.Seq != c.lastSeq+1 {
		return true
	}

	switch delta.Side {
	case engine.Buy:
		if delta.Quantity > 0 {
			c.bids[delta.Price] = delta.Quantity
		} else {
			delete(c.bids, delta.Price)
		}
	case engine.Sell:
		if delta.Quantity > 0 {
			c.asks[delta.Price] = delta.Quantity
		} else {
			delete(c.asks, delta.Price)
		}
	}

	c.lastSeq = delta.Seq

	return false
}

// sendTrade sends a new trade into the trades channel; dropped messages are ignored (trades don't carry a seq).
func (c *Client) sendTrade(data []byte) {
	var trade Trade

	if err := json.Unmarshal(data, &trade); err != nil {
		return
	}

	select {
	case c.trades <- trade:
	default:
		// drop sends into the consumer-exposed trades channel if busy
	}
}

// --- exposer methods ---

// Book returns a sorted DepthSnapshot representing the client's current book state.
func (c *Client) Book() engine.DepthSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	bids := make([]engine.PriceLevel, 0, len(c.bids))
	for price, quantity := range c.bids {
		bids = append(bids, engine.PriceLevel{Price: price, Quantity: quantity})
	}

	asks := make([]engine.PriceLevel, 0, len(c.asks))
	for price, quantity := range c.asks {
		asks = append(asks, engine.PriceLevel{Price: price, Quantity: quantity})
	}

	slices.SortFunc(bids, func(x, y engine.PriceLevel) int { return int(y.Price - x.Price) }) // high to low
	slices.SortFunc(asks, func(x, y engine.PriceLevel) int { return int(x.Price - y.Price) }) // low to high

	return engine.DepthSnapshot{Bids: bids, Asks: asks}
}

// Trades returns a channel of confirmed trades, some of which may have been dropped if the channel was busy.
func (c *Client) Trades() <-chan Trade {
	return c.trades
}
