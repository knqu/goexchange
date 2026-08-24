package engine

import "context"

// --- event sequence counter ---

type seqCounter struct {
	n uint64
}

func (s *seqCounter) next() uint64 {
	s.n++
	return s.n
}

// --- engine data structures ---

// Engine serializes all book access through a single goroutine.
type Engine struct {
	symbol string
	book   *Book
	cmds   chan Command   // owned by the engine
	events chan<- []Event // not owned by the engine; restrict to send-only
	seq    seqCounter
	halted bool
}

// NewEngine initializes an Engine with a new book and commands buffer.
func NewEngine(symbol string, buf int, events chan<- []Event) *Engine {
	return &Engine{
		symbol: symbol,
		book:   NewBook(),
		cmds:   make(chan Command, buf),
		events: events,
	}
}

// --- command sequencing logic ---

// Run is the single-writer command input loop for the book.
// The gateway must stop accepting commands before stopping the engine (otherwise the drain will never complete).
func (e *Engine) Run(ctx context.Context) {
	for {
		select {
		case cmd := <-e.cmds:
			e.handle(cmd)
		case <-ctx.Done():
			// gracefully shutdown by draining remaining buffered commands
			for {
				select {
				case cmd := <-e.cmds:
					e.handle(cmd)
				default:
					return // exit once no more commands are ready (meaning cmds is empty)
				}
			}
		}
	}
}

// handle processes a single command and emits the resulting events.
func (e *Engine) handle(cmd Command) {
	switch cmd.Type {
	case CmdHalt:
		e.halted = true
		e.events <- []Event{{Type: EventHalted, Seq: e.seq.next()}}
	case CmdResume:
		e.halted = false
		e.events <- []Event{{Type: EventResumed, Seq: e.seq.next()}}
	default:
		if e.halted && cmd.Type == CmdSubmit {
			// any command that adds liquidity (only submit currently) should be rejected on engine halt
			e.events <- reject(&e.seq, cmd.Order.ID, RejectHalted)
		} else {
			// any command that allows participants to exit or observe should be allowed even on engine halt
			e.events <- e.book.Apply(cmd, &e.seq)
		}
	}
}

// --- public helpers ---

// Restore applies commands synchronously to the book to rebuild state from a journal.
// It must be called before Run(), as concurrent use with the engine running will result in a data race.
func (e *Engine) Restore(cmds []Command) {
	for _, cmd := range cmds {
		e.book.Apply(cmd, &e.seq)
	}
}

// Cmds exposes the inbound commands channel as send-only for callers.
func (e *Engine) Cmds() chan<- Command {
	return e.cmds
}

// Depth returns the top-n bids and asks in the book as an independent snapshot.
// Results are never overwritten after being returned; safe to retain.
func (e *Engine) Depth(n int) (bids, asks []PriceLevel) {
	return e.book.Depth(n)
}

// StateHash returns a deterministic hash of the book's resting state.
// Two books with identical resting orders (prices, FIFO order, and remaining quantities) will produce the same hash.
func (e *Engine) StateHash() uint64 {
	return e.book.StateHash()
}
