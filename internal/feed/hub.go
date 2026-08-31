package feed

import "sync"

// --- subscriber data structures ---

// Subscriber represents a client that receives messages from the hub.
type Subscriber struct {
	Messages chan []byte
}

// NewSubscriber initializes a new subscriber with a messages channel of capacity buf.
func NewSubscriber(buf int) *Subscriber {
	return &Subscriber{Messages: make(chan []byte, buf)}
}

// --- hub data structures and methods ---

// Hub broadcasts a publisher's messages to its subscribers.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[*Subscriber]struct{}
}

// NewHub initializes a new hub with an empty subscribers set.
func NewHub() *Hub {
	return &Hub{subscribers: make(map[*Subscriber]struct{})}
}

// Subscribe adds a new subscriber to the hub.
func (h *Hub) Subscribe(buf int) *Subscriber {
	sub := NewSubscriber(buf)
	h.mu.Lock()
	h.subscribers[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Unsubscribe removes a subscriber from the hub and closes their message channel.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	delete(h.subscribers, sub)
	h.mu.Unlock()
	close(sub.Messages) // closing signals the consumer's range-loop to end
}

// Publish broadcasts a message to all subscribers.
// Note that slow subscribers lose messages rather than blocking the publisher.
func (h *Hub) Publish(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for sub := range h.subscribers {
		select {
		case sub.Messages <- msg:
		default:
			// drop message if subscriber's message channel is full
		}
	}
}
