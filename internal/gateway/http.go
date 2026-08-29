package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"

	"github.com/knqu/goexchange/internal/engine"
)

// --- gateway data structures and setup ---

// orderRequest is the expected JSON structure for a new order request.
type orderRequest struct {
	AgentID  uint64 `json:"agent_id"`
	Symbol   string `json:"symbol"`
	Side     string `json:"side"`
	Type     string `json:"type"`
	TIF      string `json:"tif"`
	Price    int64  `json:"price"`
	Quantity int64  `json:"quantity"`
}

// orderCancelRequest is constructed from the order cancellation request path and query parameters.
type orderCancelRequest struct {
	AgentID uint64
	Symbol  string
	OrderID uint64
}

// Gateway exposes an exchange via HTTP endpoints.
// It processes requests into validated commands and routes them to the exchange.
type Gateway struct {
	exchange    *engine.Exchange
	prevOrderID atomic.Uint64 // needs to be atomic because multiple handlers can run concurrently
}

func (g *Gateway) nextOrderID() engine.OrderID {
	return engine.OrderID(g.prevOrderID.Add(1))
}

// NewGateway initializes a new Gateway instance that wraps the given exchange.
func NewGateway(exchange *engine.Exchange) *Gateway {
	return &Gateway{exchange: exchange}
}

// Serve starts the HTTP server on the given address and handles incoming requests.
func (g *Gateway) Serve(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", g.handleNewOrder)
	mux.HandleFunc("DELETE /orders/{id}", g.handleCancelOrder)
	return http.ListenAndServe(addr, mux)
}

// --- gateway handlers ---

// handleNewOrder processes a new order request, validates it, and dispatches it to the exchange.
func (g *Gateway) handleNewOrder(w http.ResponseWriter, r *http.Request) {
	var req orderRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "malformed json: "+err.Error(), http.StatusBadRequest)
		return
	}

	cmd, err := g.parseOrder(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	accepted, err := g.exchange.TryDispatch(req.Symbol, cmd)
	if err != nil {
		http.Error(w, "unknown symbol", http.StatusNotFound)
		return
	}
	if !accepted {
		http.Error(w, "engine at capacity, retry", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{"orderId": cmd.Order.ID})
}

// handleCancelOrder processes an order cancellation request, validates it, and dispatches it to the exchange.
func (g *Gateway) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid order id in path", http.StatusBadRequest)
		return
	}

	symbol := r.URL.Query().Get("symbol")

	agentID, err := strconv.ParseUint(r.URL.Query().Get("agent_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid or missing agent_id", http.StatusBadRequest)
		return
	}

	cmd, err := g.parseCancelOrder(orderCancelRequest{AgentID: agentID, Symbol: symbol, OrderID: orderID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	accepted, err := g.exchange.TryDispatch(symbol, cmd)
	if err != nil {
		http.Error(w, "unknown symbol", http.StatusNotFound)
		return
	}
	if !accepted {
		http.Error(w, "engine at capacity, retry", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// --- parsing helpers ---

// parseOrder validates the order request and converts it into an engine command.
func (g *Gateway) parseOrder(req orderRequest) (engine.Command, error) {
	side, err := parseSide(req.Side)
	if err != nil {
		return engine.Command{}, err
	}

	orderType, err := parseType(req.Type)
	if err != nil {
		return engine.Command{}, err
	}

	tif, err := parseTIF(req.TIF)
	if err != nil {
		return engine.Command{}, err
	}

	if req.AgentID == 0 {
		return engine.Command{}, fmt.Errorf("agent id must be non-zero")
	}

	if orderType == engine.Limit && req.Price <= 0 {
		return engine.Command{}, fmt.Errorf("limit order needs positive price, got %d", req.Price)
	}
	if orderType == engine.Market && req.Price != 0 {
		return engine.Command{}, fmt.Errorf("market order must not carry a price")
	}

	if req.Quantity <= 0 {
		return engine.Command{}, fmt.Errorf("quantity must be positive, got %d", req.Quantity)
	}

	return engine.Command{
		Type: engine.CmdSubmit,
		Order: engine.Order{
			ID:       g.nextOrderID(),
			AgentID:  engine.AgentID(req.AgentID),
			Side:     side,
			Type:     orderType,
			TIF:      tif,
			Price:    req.Price,
			Quantity: req.Quantity,
		},
	}, nil
}

func (g *Gateway) parseCancelOrder(req orderCancelRequest) (engine.Command, error) {
	if req.AgentID == 0 {
		return engine.Command{}, fmt.Errorf("agent id must be non-zero")
	}

	if req.OrderID == 0 {
		return engine.Command{}, fmt.Errorf("order id must be non-zero")
	}

	return engine.Command{
		Type:     engine.CmdCancel,
		CancelID: engine.OrderID(req.OrderID),
	}, nil
}

func parseSide(s string) (engine.Side, error) {
	switch s {
	case "buy":
		return engine.Buy, nil
	case "sell":
		return engine.Sell, nil
	}
	return 0, fmt.Errorf("invalid side %q (want buy|sell)", s)
}

func parseType(s string) (engine.OrderType, error) {
	switch s {
	case "limit":
		return engine.Limit, nil
	case "market":
		return engine.Market, nil
	}
	return 0, fmt.Errorf("invalid type %q (want limit|market)", s)
}

func parseTIF(s string) (engine.TIF, error) {
	switch s {
	case "day":
		return engine.Day, nil
	case "ioc":
		return engine.IOC, nil
	case "fok":
		return engine.FOK, nil
	}
	return 0, fmt.Errorf("invalid tif %q (want day|ioc|fok)", s)
}
