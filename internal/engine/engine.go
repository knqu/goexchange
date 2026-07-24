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
	done   chan struct{} // closed when Run exits; used to wait for engine shutdown
}

// NewEngine initializes an Engine with a new book and commands buffer.
func NewEngine(symbol string, buf int, events chan<- []Event) *Engine {
	return &Engine{
		symbol: symbol,
		book:   NewBook(),
		cmds:   make(chan Command, buf),
		events: events,
		done:   make(chan struct{}),
	}
}

// Run is the single-writer command input loop for the book.
func (e *Engine) Run(ctx context.Context) {
	defer close(e.done)
	for {
		select {
		case cmd := <-e.cmds:
			e.events <- e.book.Apply(cmd, &e.seq)
		case <-ctx.Done():
			return
		}
	}
}

// Cmds exposes the inbound commands channel as send-only for callers
func (e *Engine) Cmds() chan<- Command {
	return e.cmds
}

// Done reports a channel closed when Run has fully exited.
func (e *Engine) Done() <-chan struct{} {
	return e.done
}
