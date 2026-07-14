package engine

// --- sequence counter ---

type seqCounter struct {
	n uint64
}

func (s *seqCounter) next() uint64 {
	s.n++
	return s.n
}

// --- command routing logic ---

// Apply is the entry point for all engine commands.
func (b *Book) Apply(cmd Command, seq *seqCounter) []Event {
	switch cmd.Type {
	case CmdSubmit:
		return b.applySubmit(cmd.Order, seq)
	case CmdCancel:
		return b.applyCancel(cmd.CancelID, seq)
	default:
		return []Event{{Type: EventRejected, Seq: seq.next(),
			Reason: RejectUnknownCommand}}
	}
}

// Caller guarantees IDs are never reused.
func (b *Book) applySubmit(o Order, seq *seqCounter) []Event {
	if (o.Type == Limit && o.Price <= 0) || (o.Type == Market && o.Price != 0) || o.Quantity <= 0 {
		return []Event{{Type: EventRejected, Seq: seq.next(),
			OrderID: o.ID, Reason: RejectInvalidPriceOrQuantity}}
	}

	o.Remaining = o.Quantity

	if o.TIF == FOK && !b.fillable(&o) {
		return []Event{
			{Type: EventRejected, Seq: seq.next(),
				OrderID: o.ID, Reason: RejectFOKInsufficient},
		}
	}

	events := []Event{{Type: EventAccepted, Seq: seq.next(),
		OrderID: o.ID, Side: o.Side, Price: o.Price, Quantity: o.Quantity}}
	events = append(events, b.matchLoop(&o, seq)...)

	if o.Remaining > 0 && o.Type == Limit && o.TIF == Day {
		b.rest(&o) // resting is safe because o is a local copy
	}

	return events
}

func (b *Book) applyCancel(id OrderID, seq *seqCounter) []Event {
	o, ok := b.cancel(id)
	if !ok {
		return []Event{{Type: EventRejected, Seq: seq.next(),
			OrderID: id, Reason: RejectOrderNotFound}}
	}
	return []Event{{Type: EventCanceled, Seq: seq.next(),
		OrderID: o.ID, Price: o.Price, Quantity: o.Remaining}}
}

// --- matching logic ---

func (b *Book) matchLoop(o *Order, seq *seqCounter) []Event {
	opp := b.sideFor(o.Side.other())
	var events []Event

	for o.Remaining > 0 && len(opp.levels) > 0 {
		best := opp.levels[0]

		if !crosses(o, best.price) {
			break
		}

		for node := best.orders.Front(); node != nil && o.Remaining > 0; {
			maker := node.Value.(*Order)
			quantity := min(o.Remaining, maker.Remaining)

			o.Remaining -= quantity
			maker.Remaining -= quantity
			best.volume -= quantity

			events = append(events, Event{
				Type: EventTraded, Seq: seq.next(),
				OrderID: o.ID, MakerOrderID: maker.ID, Side: o.Side, Price: maker.Price, Quantity: quantity,
			})

			nextNode := node.Next() // we need to keep a reference to `node` to be able to delete it
			if maker.Remaining == 0 {
				best.orders.Remove(node)
				delete(b.byID, maker.ID)
			}
			node = nextNode
		}

		if best.orders.Len() == 0 {
			opp.removeLevelAt(0) // best level is always index 0
		}
	}

	return events
}

// --- helpers ---

// crosses returns whether taker o would trade at given maker price.
func crosses(o *Order, price int64) bool {
	if o.Type == Market {
		return true
	}
	if o.Side == Buy {
		return price <= o.Price
	}
	return price >= o.Price
}

func (s Side) other() Side {
	if s == Buy {
		return Sell
	}
	return Buy
}

// fillable returns whether an order can be completely filled from existing liquidity.
// Caller guarantees that o.Remaining has been initialized to o.Quantity.
func (b *Book) fillable(o *Order) bool {
	opp := b.sideFor(o.Side.other())
	availableVolume := int64(0)

	for _, lvl := range opp.levels {
		if availableVolume >= o.Remaining {
			return true
		}
		if !crosses(o, lvl.price) {
			break
		}
		availableVolume += lvl.volume
	}

	return availableVolume >= o.Remaining
}
