package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/knqu/goexchange/internal/engine"
)

// --- request/response data structures ---

// orderRequest mirrors the gateway's expected JSON body for a new order request.
type orderRequest struct {
	AgentID  uint64 `json:"agent_id"`
	Symbol   string `json:"symbol"`
	Side     string `json:"side"`
	Type     string `json:"type"`
	TIF      string `json:"tif"`
	Price    int64  `json:"price"`
	Quantity int64  `json:"quantity"`
}

// orderResponse mirrors the gateway's 202 response body.
type orderResponse struct {
	OrderID uint64 `json:"orderId"`
}

// --- gateway setup and methods ---

// GatewayClient allows agents to send actions to the exchange's public order API as HTTP requests.
type GatewayClient struct {
	baseURL string
	agentID engine.AgentID
	http    *http.Client
}

// NewGatewayClient initializes a client that submits orders for the agent with the given ID.
func NewGatewayClient(baseURL string, agentID engine.AgentID) *GatewayClient {
	return &GatewayClient{
		baseURL: baseURL,
		agentID: agentID,
		http: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// Do sends an action to the exchange, executing it and returning a newly-minted OrderID for submits (0 for cancels).
func (g *GatewayClient) Do(symbol string, action Action) (engine.OrderID, error) {
	switch action.Type {
	case ActionSubmit:
		return g.submit(symbol, action)
	case ActionCancel:
		return 0, g.cancel(symbol, action.CancelID)
	default:
		return 0, fmt.Errorf("unknown action type %d", action.Type)
	}
}

// --- request handlers ---

func (g *GatewayClient) submit(symbol string, action Action) (engine.OrderID, error) {
	req := orderRequest{
		AgentID:  uint64(g.agentID),
		Symbol:   symbol,
		Side:     action.Side.String(),
		Type:     action.OrderType.String(),
		TIF:      action.TIF.String(),
		Price:    action.Price,
		Quantity: action.Quantity,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("encoding order: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, g.baseURL+"/orders", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("building submit request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpRes, err := g.http.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("submitting order: %w", err) // transport failure
	}
	defer httpRes.Body.Close()

	if httpRes.StatusCode != http.StatusAccepted {
		return 0, statusError("submit", httpRes)
	}

	var res orderResponse
	if err := json.NewDecoder(httpRes.Body).Decode(&res); err != nil {
		return 0, fmt.Errorf("decoding order id: %w", err)
	}

	return engine.OrderID(res.OrderID), nil
}

func (g *GatewayClient) cancel(symbol string, orderID engine.OrderID) error {
	path := fmt.Sprintf("%s/orders/%d?symbol=%s&agent_id=%d", g.baseURL, orderID, symbol, g.agentID)

	httpReq, err := http.NewRequest(http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("building cancel request: %w", err)
	}

	httpRes, err := g.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("canceling order %d: %w", orderID, err)
	}
	defer httpRes.Body.Close()

	if httpRes.StatusCode != http.StatusAccepted {
		return statusError("cancel", httpRes)
	}

	return nil
}

// --- helpers ---

// statusError parses a rejection response from the server into a readable error.
func statusError(operation string, res *http.Response) error {
	message, _ := io.ReadAll(io.LimitReader(res.Body, 256))
	return fmt.Errorf("%s rejected (%s): %s", operation, res.Status, bytes.TrimSpace(message))
}
