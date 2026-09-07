package agents

import (
	"testing"

	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/execution"
)

// --- helpers ---

// fill constructs an execution report for the given symbol.
func fill(symbol string, side engine.Side, price, quantity int64) execution.Fill {
	return execution.Fill{Symbol: symbol, Side: side, Price: price, Quantity: quantity}
}

// checkPosition fails the test if the position for symbol doesn't match the expected values.
func checkPosition(t *testing.T, p *Portfolio, symbol string, quantity, totalCost, realized int64) {
	t.Helper()

	pos, ok := p.Positions[symbol]
	if !ok {
		t.Fatalf("no position for %s", symbol)
	}

	if pos.Quantity != quantity {
		t.Errorf("%s quantity = %d, want %d", symbol, pos.Quantity, quantity)
	}
	if pos.TotalCost != totalCost {
		t.Errorf("%s total cost = %d, want %d", symbol, pos.TotalCost, totalCost)
	}
	if pos.Realized != realized {
		t.Errorf("%s realized = %d, want %d", symbol, pos.Realized, realized)
	}
}

// checkCash fails the test if the portfolio's cash doesn't match the expected amount.
func checkCash(t *testing.T, p *Portfolio, want int64) {
	t.Helper()

	if p.Cash != want {
		t.Errorf("cash = %d, want %d", p.Cash, want)
	}
}

// --- position transition tests ---

// TestLongRoundTrip opens a long position, then fully closes it at a higher price.
func TestLongRoundTrip(t *testing.T) {
	p := NewPortfolio(100_000)

	p.OnFill(fill("ACME", engine.Buy, 9950, 10))
	checkCash(t, p, 500)
	checkPosition(t, p, "ACME", 10, 99_500, 0)

	p.OnFill(fill("ACME", engine.Sell, 10_000, 10)) // sell at higher price
	checkCash(t, p, 100_500)
	checkPosition(t, p, "ACME", 0, 0, 500) // realized 500 profit (50/unit * 10 units sold); position flattens

	if got := p.Positions["ACME"].AverageCost(); got != 0 {
		t.Errorf("flat average cost = %d, want 0", got)
	}

	if got := p.Positions["ACME"].Unrealized(10_000); got != 0 {
		t.Errorf("flat unrealized = %d, want 0", got)
	}
}

// TestShortRoundTrip opens a short position, then covers it at a lower price; verifies signs in Portfolio.OnFill().
func TestShortRoundTrip(t *testing.T) {
	p := NewPortfolio(100_000)

	p.OnFill(fill("ACME", engine.Sell, 9950, 10))
	checkCash(t, p, 199_500)
	checkPosition(t, p, "ACME", -10, -99_500, 0)

	// average cost of a short should be the same as sale price (negative cost / negative quantity)
	if got := p.Positions["ACME"].AverageCost(); got != 9950 {
		t.Errorf("short average cost = %d, want 9950", got)
	}

	p.OnFill(fill("ACME", engine.Buy, 9900, 10)) // buy back at lower price
	checkCash(t, p, 100_500)
	checkPosition(t, p, "ACME", 0, 0, 500) // realized 500 profit (50/unit * 10 units bought); position flattens

	if got := p.Positions["ACME"].AverageCost(); got != 0 {
		t.Errorf("flat average cost = %d, want 0", got)
	}

	if got := p.Positions["ACME"].Unrealized(10_000); got != 0 {
		t.Errorf("flat unrealized = %d, want 0", got)
	}
}

// TestCrossThroughZeroLongToShort sells more than is held, flattening the long and opening a short.
func TestCrossThroughZeroLongToShort(t *testing.T) {
	p := NewPortfolio(100_000)

	p.OnFill(fill("ACME", engine.Buy, 9950, 10))

	// closes 10 long (realizing 500 profit), then opens 5 short at 10,000
	p.OnFill(fill("ACME", engine.Sell, 10_000, 15))
	checkCash(t, p, 150_500)                      // 500 (cash after first fill) + 150,000 (10,000 * 15 sold)
	checkPosition(t, p, "ACME", -5, -50_000, 500) // new short's basis is the crossing price

	// the new short's average cost should be the price it was opened at (not the old long's basis)
	if got := p.Positions["ACME"].AverageCost(); got != 10_000 {
		t.Errorf("post-cross average cost = %d, want 10000", got)
	}

	// the new short should start flat (no unrealized P&L at the price it opened at)
	if got := p.Positions["ACME"].Unrealized(10_000); got != 0 {
		t.Errorf("post-cross unrealized = %d, want 0", got)
	}
}

// TestCrossThroughZeroShortToLong buys more than is owed, covering the short and opening a long.
func TestCrossThroughZeroShortToLong(t *testing.T) {
	p := NewPortfolio(100_000)

	p.OnFill(fill("ACME", engine.Sell, 10_000, 10))

	// covers 10 short (realizing 1,000 profit), then opens 5 long at 9,900
	p.OnFill(fill("ACME", engine.Buy, 9900, 15))
	checkCash(t, p, 51_500)                      // 200,000 (cash after first fill) - 148,500 (9,900 * 15 bought)
	checkPosition(t, p, "ACME", 5, 49_500, 1000) // new long's basis is the crossing price

	// the new long's average cost should be the price it was opened at (not the old short's basis)
	if got := p.Positions["ACME"].AverageCost(); got != 9900 {
		t.Errorf("post-cross average cost = %d, want 9900", got)
	}

	// the new long should start flat (no unrealized P&L at the price it opened at)
	if got := p.Positions["ACME"].Unrealized(9900); got != 0 {
		t.Errorf("post-cross unrealized = %d, want 0", got)
	}
}

// TestPartialClose closes part of a long position, leaving the remainder open at the original average cost.
func TestPartialClose(t *testing.T) {
	p := NewPortfolio(100_000)

	p.OnFill(fill("ACME", engine.Buy, 9950, 10))

	// close 4 (realizing 200 profit) and keep 6 at the same average cost
	p.OnFill(fill("ACME", engine.Sell, 10_000, 4))
	checkCash(t, p, 40_500)                     // 500 (cash after first fill) + 40,000 (10,000 * 4 sold)
	checkPosition(t, p, "ACME", 6, 59_700, 200) // 59,700 = 9,950 * 6

	// remaining position maintains the original average cost (not the exit price)
	if got := p.Positions["ACME"].AverageCost(); got != 9950 {
		t.Errorf("average cost after partial close = %d, want 9950", got)
	}

	// unrealized should equal (10,000 - 9,950) * 6 = 300
	if got := p.Positions["ACME"].Unrealized(10_000); got != 300 {
		t.Errorf("unrealized after partial close = %d, want 300", got)
	}
}

// --- summary tests ---

// TestSummaryAccounting verifies that realizing a gain moves values between columns without changing net worth.
func TestSummaryAccounting(t *testing.T) {
	p := NewPortfolio(100_000)

	p.OnFill(fill("ACME", engine.Buy, 9950, 10))

	// at cost: no gain, so net worth is unchanged from starting cash; net worth = 500 (cash) + 99,500 (market value)
	s := p.Summary(map[string]int64{"ACME": 9950})
	if s.NetWorth != 100_000 {
		t.Errorf("net worth at cost = %d, want 100000", s.NetWorth)
	}
	if s.TotalUnrealized != 0 {
		t.Errorf("unrealized at cost = %d, want 0", s.TotalUnrealized)
	}

	// price rises: the whole gain (500) is unrealized; net worth = 500 (cash) + 100,000 (market value)
	s = p.Summary(map[string]int64{"ACME": 10_000})
	if s.NetWorth != 100_500 {
		t.Errorf("net worth after mark up = %d, want 100500", s.NetWorth)
	}
	if s.TotalUnrealized != 500 || s.TotalRealized != 0 {
		t.Errorf("realized/unrealized = %d/%d, want 0/500", s.TotalRealized, s.TotalUnrealized)
	}

	// realize partial gains (should not change net worth; it only moves a quantity from unrealized to cash)
	p.OnFill(fill("ACME", engine.Sell, 10_000, 4))

	// realized 4 shares: 40,000 liquidated to cash (200 profit); net worth = 40,500 (cash) + 60,000 (market value)
	s = p.Summary(map[string]int64{"ACME": 10_000})
	if s.NetWorth != 100_500 {
		t.Errorf("net worth after partial close = %d, want 100500 (unchanged)", s.NetWorth)
	}
	if s.TotalRealized != 200 || s.TotalUnrealized != 300 {
		t.Errorf("realized/unrealized = %d/%d, want 200/300", s.TotalRealized, s.TotalUnrealized)
	}
}
