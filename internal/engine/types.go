package engine

// --- order types ---

type Side uint8

const (
	Buy Side = iota
	Sell
)

func (s Side) String() string {
	switch s {
	case Buy:
		return "buy"
	case Sell:
		return "sell"
	}
	return ""
}

func (s Side) Other() Side {
	if s == Buy {
		return Sell
	}
	return Buy
}

type OrderType uint8

const (
	Limit OrderType = iota
	Market
)

func (ot OrderType) String() string {
	switch ot {
	case Limit:
		return "limit"
	case Market:
		return "market"
	}
	return ""
}

type TIF uint8 // time in force

const (
	Day TIF = iota // rest in the book until filled or cancelled
	IOC            // immediately attempt to execute; any unfilled portion is cancelled
	FOK            // immediately attempt to fill the order completely; if unable, the entire order is cancelled
)

func (tif TIF) String() string {
	switch tif {
	case Day:
		return "day"
	case IOC:
		return "immediate-or-cancel"
	case FOK:
		return "fill-or-kill"
	}
	return ""
}

type OrderID uint64 // starts at 1; should never be sent into engine as 0
type AgentID uint64 // starts at 1; should never be sent into engine as 0

type Order struct {
	ID        OrderID
	AgentID   AgentID
	Side      Side
	Type      OrderType
	TIF       TIF
	Price     int64
	Quantity  int64
	Remaining int64
	Seq       uint64 // maintains time priority
}

// --- event types ---

type EventType uint8

const (
	EventAccepted EventType = iota
	EventRejected
	EventTraded
	EventCanceled
	EventRested
	EventExpired
	EventHalted
	EventResumed
)

type CancelReason uint8

const (
	CancelNone CancelReason = iota
	CancelUser
	CancelSelfTrade
)

type RejectReason uint8

const (
	RejectNone RejectReason = iota
	RejectUnknownCommand
	RejectInvalidPrice
	RejectInvalidQuantity
	RejectOrderNotFound
	RejectFOKInsufficient
	RejectHalted
	RejectJournalFailed
)

type Event struct {
	Type         EventType
	Seq          uint64
	OrderID      OrderID
	MakerOrderID OrderID
	AgentID      AgentID
	MakerAgentID AgentID
	Side         Side
	Price        int64
	Quantity     int64
	CancelReason CancelReason
	RejectReason RejectReason
}

// --- command types ---

type CmdType uint8

const (
	CmdSubmit CmdType = iota
	CmdCancel
	CmdHalt
	CmdResume
	CmdDepth
)

// Command is an instruction to the engine.
type Command struct {
	Type       CmdType
	Order      Order      // CmdSubmit (passed by value; engine owns its own copy)
	CancelID   OrderID    // CmdCancel
	DepthQuery DepthQuery `json:"-"` // CmdDepth (exclude from JSON: never journaled + channels break serialization)
}

// --- depth types ---

type DepthQuery struct {
	Depth int
	Reply chan<- DepthSnapshot // needed because CmdDepth is queued and answered in sequence with trading
}

// PriceLevel represents one rung of a depth ladder: total resting quantity at a price.
type PriceLevel struct {
	Price    int64
	Quantity int64
}

type DepthSnapshot struct {
	Bids []PriceLevel
	Asks []PriceLevel
}
