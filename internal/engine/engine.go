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

// --- journal writer interface ---

// JournalWriter is an interface for appending command records to a symbol-specific journal file.
type JournalWriter interface {
	Append(cmd Command) error
	Sync() error
	Close() error
}

// --- engine data structures ---

// Engine serializes all book access through a single goroutine.
type Engine struct {
	symbol  string
	book    *Book
	cmds    chan Command   // owned by the engine
	events  chan<- []Event // not owned by the engine; restrict to send-only
	journal JournalWriter
	seq     seqCounter
	halted  bool
	cancel  context.CancelFunc // kill switch shared by all engines, called if journal ever fails to write
}

// NewEngine initializes an Engine with a new book and commands buffer.
func NewEngine(symbol string, buf int, events chan<- []Event, journal JournalWriter, cancel context.CancelFunc) *Engine {
	return &Engine{
		symbol:  symbol,
		book:    NewBook(),
		cmds:    make(chan Command, buf),
		events:  events,
		journal: journal,
		cancel:  cancel,
	}
}

// --- command sequencing logic ---

// Run is the single-writer command input loop for the book.
// The gateway must stop accepting commands before stopping the engine (otherwise the drain will never complete).
func (e *Engine) Run(ctx context.Context) {
	for {
		select {
		case cmd := <-e.cmds:
			e.events <- e.handle(cmd)
		case <-ctx.Done():
			// gracefully shutdown by draining remaining buffered commands
			for {
				select {
				case cmd := <-e.cmds:
					e.events <- e.handle(cmd)
				default:
					return // exit once no more commands are ready (meaning cmds is empty)
				}
			}
		}
	}
}

// handle journals and applies a single command, returning the resulting events to be emitted by the caller.
func (e *Engine) handle(cmd Command) []Event {
	switch cmd.Type {
	case CmdHalt:
		e.halted = true
		return []Event{{Type: EventHalted, Seq: e.seq.next()}}
	case CmdResume:
		e.halted = false
		return []Event{{Type: EventResumed, Seq: e.seq.next()}}
	default:
		// any command that adds liquidity (only submit currently) should be rejected on engine halt
		if e.halted && cmd.Type == CmdSubmit {
			return reject(&e.seq, cmd.Order.ID, RejectHalted)
		}

		// any command that allows participants to exit or observe should be allowed even on engine halt
		if e.journal != nil {
			if err := e.journal.Append(cmd); err != nil {
				e.cancel()
				return reject(&e.seq, cmd.Order.ID, RejectJournalFailed) // do not apply command
			}
		}
		return e.book.Apply(cmd, &e.seq)
	}
}

// --- public helpers ---

// Restore applies commands synchronously to the book to rebuild state from a journal.
// It must be called before Run(), as concurrent use with the engine running will result in a data race.
func (e *Engine) Restore(cmds []Command) {
	for _, cmd := range cmds {
		// call Book.Apply() directly; do not write to journal (would duplicate already-journaled commands)
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
