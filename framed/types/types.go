package types

import (
	"sync/atomic"
)

// Price is a struct embedding atomic.Int64 for thread-safe price values.
type Price struct {
	atomic.Int64
}

// Quantity is a struct embedding atomic.Int64 for thread-safe quantity values.
type Quantity struct {
	atomic.Int64
}

// NewPrice creates a new Price with an initial value and returns a pointer.
func NewPrice(val int64) *Price {
	p := &Price{}
	p.Store(val)
	return p
}

// NewQuantity creates a new Quantity with an initial value and returns a pointer.
func NewQuantity(val int64) *Quantity {
	q := &Quantity{}
	q.Store(val)
	return q
}

// Instrument represents a trading instrument.
type Instrument struct {
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
	Sequence       atomic.Int64         // Atomic for thread-safe updates
}

// OrderBookWithVWAP combines an order book with its VWAP.
type OrderBookWithVWAP struct {
	OrderBook *OrderBook
	VWAP      *Price
}