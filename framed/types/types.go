package types

import (
	"sync/atomic"
	
)

const SCALE_FACTOR = 1_000_000

// Price is a struct embedding atomic.Int64 for thread-safe price values.
type Price struct {
	atomic.Int64
}

// Quantity is a struct embedding atomic.Int64 for thread-safe quantity values.
type Quantity struct {
	atomic.Int64
}

type ExchangeID string

const (
    ExchangeIDBybit     ExchangeID = "bybit"
    ExchangeIDInjective ExchangeID = "injective"
)





// NewPrice creates a new Price with an initial value and returns a pointer.
func NewPrice(val int64) *Price {
	p := &Price{}
	p.Store(val)
	return p
}

// NewQuantity creates a new Quantity with an initial value and returns a pointer.
func NewQuantity(val float64) *Quantity {
    q := &Quantity{}
    q.Store(int64(val * SCALE_FACTOR))
    return q
}

func (q *Quantity) ToFloat64() float64 {
    return float64(q.Load()) / SCALE_FACTOR
}

// Instrument represents a trading instrument.
type Instrument struct {
	Symbol        string
	BaseCurrency  string
	QuoteCurrency string
	MinLotSize    *Quantity
	ContractType  string
}

// OrderBook represents the state of an order book with atomic fields.
type OrderBook struct {
	Instrument     *Instrument
	Asks           map[*Price]*Quantity // Maps are not locked as a whole; individual levels updated atomically
	Bids           map[*Price]*Quantity // Maps are not locked as a whole; individual levels updated atomically
	LastUpdateTime atomic.Int64         // Atomic for thread-safe updates
	Sequence       atomic.Int64
	Exchange       *ExchangeID  
}

// OrderBookWithVWAP combines an order book with its VWAP.
type OrderBookWithVWAP struct {
	OrderBook *OrderBook
	VWAP      *Price
}

// Order represents a trading order.
type Order struct {
    ExchangeID ExchangeID   // Added ExchangeID
    Instrument *Instrument
    Price      *Price
    Quantity   *Quantity
    Side       string // "Buy" or "Sell"
}