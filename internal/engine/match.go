package engine

// --- sequence counter ---

type seqCounter struct {
	n uint64
}

func (s *seqCounter) next() uint64 {
	s.n++
	return s.n
}

// --- command handlers ---

// Apply is the entry point for all engine commands.
func (b *Book) Apply(cmd Command, seq *seqCounter) []Event {
	switch cmd.Type {
	case CmdSubmit:
		return b.applySubmit(cmd.Order, seq)
	case CmdCancel:
		return b.applyCancel(cmd.CancelID, seq)
	default:
		return reject(seq, 0, RejectUnknownCommand)
	}
}

// Caller guarantees IDs are never reused.
func (b *Book) applySubmit(o Order, seq *seqCounter) []Event {
	if _, exists := b.byID[o.ID]; exists {
		panic("duplicate order id")
	}

	// if price and quantity are both invalid, RejectInvalidPrice will be caught first
	if (o.Type == Limit && o.Price <= 0) || (o.Type == Market && o.Price != 0) {
		return reject(seq, o.ID, RejectInvalidPrice)
	}
	if o.Quantity <= 0 {
		return reject(seq, o.ID, RejectInvalidQuantity)
	}

	o.Remaining = o.Quantity

	if o.TIF == FOK && !b.fillable(&o) {
		return reject(seq, o.ID, RejectFOKInsufficient)
	}

	events := []Event{{Type: EventAccepted, Seq: seq.next(),
		OrderID: o.ID, Side: o.Side, Price: o.Price, Quantity: o.Quantity}}
	events = append(events, b.matchLoop(&o, seq)...)

	if o.Remaining > 0 {
		if o.Type == Limit && o.TIF == Day {
			b.rest(&o) // resting is safe because o is a local copy
		} else {
			// unfilled remainders of market and IOC orders are discarded
			events = append(events, Event{Type: EventExpired, Seq: seq.next(),
				OrderID: o.ID, Side: o.Side, Price: o.Price, Quantity: o.Remaining})
		}
	}

	return events
}

func (b *Book) applyCancel(id OrderID, seq *seqCounter) []Event {
	o, ok := b.cancel(id)
	if !ok {
		return reject(seq, id, RejectOrderNotFound) // echo requested OrderID (no actual order exists)
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

			// prevent agents from trading against their own liquidity by cancelling resting orders
			if o.AgentID == maker.AgentID {
				nextNode := node.Next()

				best.orders.Remove(node)
				best.volume -= maker.Remaining
				delete(b.byID, maker.ID)

				events = append(events, Event{Type: EventCanceled, Seq: seq.next(),
					OrderID: maker.ID, Price: maker.Price, Quantity: maker.Remaining, Reason: CancelSelfTrade})

				node = nextNode
				continue
			}

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
		if !crosses(o, lvl.price) {
			break
		}
		for node := lvl.orders.Front(); node != nil; node = node.Next() {
			if availableVolume >= o.Remaining {
				return true
			}
			maker := node.Value.(*Order)
			if o.AgentID != maker.AgentID {
				availableVolume += maker.Remaining
			}
		}
	}

	return availableVolume >= o.Remaining
}

func reject(seq *seqCounter, id OrderID, reason RejectReason) []Event {
	return []Event{{Type: EventRejected, Seq: seq.next(), OrderID: id, Reason: reason}}
}
