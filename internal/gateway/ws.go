package gateway

import (
	"net/http"

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

	sub := aggregator.Subscribe(g.messagesBuf)
	defer aggregator.Unsubscribe(sub) // unsubscribe on disconnect (closes subscriber's messages channel)

	ctx := r.Context() // canceled when the client disconnects
	for {
		select {
		case msg, ok := <-sub.Messages:
			if !ok {
				return
			}
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
