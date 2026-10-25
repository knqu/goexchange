package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// handleWS upgrades a subscriber's HTTP connection to WebSocket and sends snapshots/deltas from the aggregator.
func (g *Gateway) handleWS(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")

	aggregator, ok := g.aggregators[symbol]
	if !ok {
		http.Error(w, "unknown symbol", http.StatusNotFound)
		return
	}

	// upgrade connection to websocket
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // websocket.Accept() already wrote an error response
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := conn.CloseRead(r.Context()) // read incoming frames so client closes/disconnects cancel ctx

	sub := aggregator.Subscribe(g.messagesBuf)
	defer aggregator.Unsubscribe(sub) // unsubscribe on disconnect (closes subscriber's messages channel)

	for {
		select {
		case msg, ok := <-sub.Messages:
			if !ok {
				return
			}

			// set a 5s timeout on every write to prevent blocking on a client whose tcp buffer is full
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()

			// exit handler if timeout expired or write otherwise failed (connection closed by client or broken)
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
