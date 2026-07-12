package engine

// --- order types ---

type Side uint8

const (
	Buy Side = iota
	Sell
)

type OrderType uint8

const (
	Limit OrderType = iota
	Market
)

type TIF uint8 // time in force

const (
	Day TIF = iota // rest in the book until filled or cancelled
	IOC            // immediately attempt to execute; any unfilled portion is cancelled
	FOK            // immediately attempt to fill the order completely; if unable, the entire order is cancelled
)

type OrderID uint64

type Order struct {
	ID        OrderID
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
)

type RejectReason uint8

const (
	RejectNone RejectReason = iota
	RejectOrderNotFound
	RejectFOKInsufficient
)

type Event struct {
	Type         EventType
	Seq          uint64
	OrderID      OrderID
	MakerOrderID OrderID
	Side         Side
	Price        int64
	Quantity     int64
	Reason       RejectReason
}
