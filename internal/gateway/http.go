package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"

	"github.com/knqu/goexchange/internal/engine"
)

// --- order request schema ---

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

// --- http routes ---

// RegisterRoutes adds the gateway's routes to mux; caller owns both mux and http.Server (and its lifecycle/shutdown).
func (g *Gateway) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /orders", g.handleNewOrder)
	mux.HandleFunc("DELETE /orders/{id}", g.handleCancelOrder)
	mux.HandleFunc("POST /admin/symbols/{symbol}/halt", g.handleHaltTrading)
	mux.HandleFunc("POST /admin/symbols/{symbol}/resume", g.handleResumeTrading)
	mux.HandleFunc("GET /books/{symbol}", g.handleDepthQuery)
	mux.HandleFunc("GET /ws", g.handleWS)
	mux.HandleFunc("GET /admin/symbols", g.handleSymbols)
}

// --- http handlers ---

func (g *Gateway) handleNewOrder(w http.ResponseWriter, r *http.Request) {
	var req orderRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "malformed json: "+err.Error(), http.StatusBadRequest)
		return
	}

	side, err := parseSide(req.Side)
	if err != nil {
		writeError(w, err)
		return
	}

	orderType, err := parseType(req.Type)
	if err != nil {
		writeError(w, err)
		return
	}

	tif, err := parseTIF(req.TIF)
	if err != nil {
		writeError(w, err)
		return
	}

	orderID, err := g.SubmitOrder(req.Symbol, engine.Order{
		// ID is set by g.SubmitOrder()
		AgentID:  engine.AgentID(req.AgentID),
		Side:     side,
		Type:     orderType,
		TIF:      tif,
		Price:    req.Price,
		Quantity: req.Quantity,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{"orderId": orderID})
}

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

	if err := g.CancelOrder(engine.AgentID(agentID), symbol, engine.OrderID(orderID)); err != nil {
		writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (g *Gateway) handleHaltTrading(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")

	if err := g.dispatch(symbol, engine.Command{Type: engine.CmdHalt}); err != nil {
		writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (g *Gateway) handleResumeTrading(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")

	if err := g.dispatch(symbol, engine.Command{Type: engine.CmdResume}); err != nil {
		writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (g *Gateway) handleDepthQuery(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")

	depth, err := strconv.Atoi(r.URL.Query().Get("depth"))
	if err != nil {
		http.Error(w, "invalid or missing depth", http.StatusBadRequest)
		return
	}

	// depth queries are queued alongside other commands and answered in sequence with live trading
	reply := make(chan engine.DepthSnapshot, 1) // set capacity to 1 so engine never blocks sending

	if err := g.dispatch(symbol, engine.Command{Type: engine.CmdDepth, DepthQuery: engine.DepthQuery{
		Depth: depth,
		Reply: reply,
	}}); err != nil {
		writeError(w, err)
		return
	}

	var depthSnapshot engine.DepthSnapshot
	select {
	case depthSnapshot = <-reply:
	case <-g.exchange.Done(): // stop waiting for a reply if the engine has already shut down
		http.Error(w, "exchange stopped", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(depthSnapshot)
}

func (g *Gateway) handleSymbols(w http.ResponseWriter, r *http.Request) {
	symbols := slices.Sorted(maps.Keys(g.aggregators)) // all tradeable symbols

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string][]string{"symbols": symbols})
}

// --- helpers ---

// writeError responds with the HTTP status corresponding to a gateway error category.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, ErrUnknownSymbol):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrQueueFull):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseSide(s string) (engine.Side, error) {
	switch s {
	case "buy":
		return engine.Buy, nil
	case "sell":
		return engine.Sell, nil
	}
	return 0, fmt.Errorf("%w: invalid side %q (want buy|sell)", ErrInvalidRequest, s)
}

func parseType(s string) (engine.OrderType, error) {
	switch s {
	case "limit":
		return engine.Limit, nil
	case "market":
		return engine.Market, nil
	}
	return 0, fmt.Errorf("%w: invalid type %q (want limit|market)", ErrInvalidRequest, s)
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
	return 0, fmt.Errorf("%w: invalid tif %q (want day|ioc|fok)", ErrInvalidRequest, s)
}
