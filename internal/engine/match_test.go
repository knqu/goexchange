package engine

import (
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

// submit runs one order through Apply and fails the test if the book's invariants break afterward.
func submit(t *testing.T, b *Book, seq *seqCounter, o Order) []Event {
	t.Helper()

	events := b.Apply(Command{Type: CmdSubmit, Order: o}, seq)
	if err := b.audit(); err != nil {
		t.Fatalf("book invariant broken after order %d: %v", o.ID, err)
	}
	return events
}
