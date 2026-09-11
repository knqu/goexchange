package agents

import (
	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/execution"
)

// --- position ---

type Position struct {
	Quantity  int64
	TotalCost int64
	Realized  int64
}

// AverageCost returns the average cost per unit of the position, or zero if the position is flat.
func (p *Position) AverageCost() int64 {
	if p.Quantity == 0 {
		return 0
	}
	return p.TotalCost / p.Quantity
}

// MarketValue returns the current market value of the position given its last price.
func (p *Position) MarketValue(last int64) int64 {
	return last * p.Quantity
}

// Unrealized returns the unrealized profit/loss of the position given its last price.
func (p *Position) Unrealized(last int64) int64 {
	return p.MarketValue(last) - p.TotalCost
}

// --- portfolio ---

type Portfolio struct {
	Cash      int64
	Positions map[string]*Position
}

// NewPortfolio initializes a new portfolio with the given cash balance and no positions.
func NewPortfolio(cash int64) *Portfolio {
	return &Portfolio{Cash: cash, Positions: make(map[string]*Position)}
}

// Position returns the amount of the given symbol currently held in the portfolio.
func (p *Portfolio) Position(symbol string) int64 {
	if pos, ok := p.Positions[symbol]; !ok {
		return pos.Quantity
	}
	return 0
}

// OnFill updates the portfolio's internal positions ledger given a confirmed order execution.
func (p *Portfolio) OnFill(fill execution.Fill) {
	pos, ok := p.Positions[fill.Symbol]
	if !ok {
		// create a new position registered to the executed order's symbol if it doesn't exist
		pos = &Position{}
		p.Positions[fill.Symbol] = pos
	}

	delta := fill.Quantity // signed change to position quantity (positive for buys, negative for sells)
	if fill.Side == engine.Sell {
		delta = -delta
	}

	p.Cash -= fill.Price * delta // cash always moves opposite to position change

	switch {
	case pos.Quantity == 0 || (delta > 0 && pos.Quantity > 0) || (delta < 0 && pos.Quantity < 0):
		// accumulate a new or existing position (starting from zero, or buying when long / selling when short)
		pos.Quantity += delta
		pos.TotalCost += fill.Price * delta
	case abs(delta) <= abs(pos.Quantity):
		// reduce an existing position (selling when long / buying when short)
		average := pos.AverageCost()
		pos.Quantity += delta
		closed := -delta
		pos.TotalCost -= average * closed
		pos.Realized += (fill.Price - average) * closed
	default:
		// cross through zero (sell more than owned when long, or buy more than owed when short)
		average := pos.AverageCost()
		pos.Realized += (fill.Price - average) * pos.Quantity
		remainder := pos.Quantity + delta
		pos.Quantity = remainder
		pos.TotalCost = fill.Price * remainder
	}
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// --- portfolio summary ---

type PositionSummary struct {
	Symbol      string
	Quantity    int64
	AverageCost int64
	Realized    int64 // profit/loss of closed positions (locked-in)
	Unrealized  int64 // profit/loss of open positions based on latest market price
}

type PortfolioSummary struct {
	Cash            int64
	Positions       []PositionSummary
	TotalRealized   int64
	TotalUnrealized int64
	NetWorth        int64    // cash + total market value of all open positions
	Unpriced        []string // positions with no last price; excluded from TotalUnrealized and NetWorth sums
}

// Summary generates a snapshot of the portfolio's state and performance given the last market price for each symbol.
func (p *Portfolio) Summary(lastPrices map[string]int64) PortfolioSummary {
	summary := PortfolioSummary{Cash: p.Cash, NetWorth: p.Cash}

	for symbol, pos := range p.Positions {
		ps := PositionSummary{
			Symbol:      symbol,
			Quantity:    pos.Quantity,
			AverageCost: pos.AverageCost(),
			Realized:    pos.Realized,
		}

		summary.TotalRealized += pos.Realized

		if last, ok := lastPrices[symbol]; ok {
			ps.Unrealized = pos.Unrealized(last)
			summary.TotalUnrealized += ps.Unrealized
			summary.NetWorth += pos.MarketValue(last)
		} else {
			summary.Unpriced = append(summary.Unpriced, symbol)
		}

		summary.Positions = append(summary.Positions, ps)
	}

	return summary
}
