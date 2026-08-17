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

// --- command sequencing logic ---

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

// Cmds exposes the inbound commands channel as send-only for callers.
func (e *Engine) Cmds() chan<- Command {
	return e.cmds
}
