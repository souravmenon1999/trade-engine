// types/types.go
package types

import "sync"

// Price represents a scaled price value. Scaled by 1,000,000
type Price int64

// Quantity represents a scaled quantity value. Scaled by 1,000,000
type Quantity int64

// Instrument defines a trading instrument.
type Instrument struct {
	BaseCurrency  string
	QuoteCurrency string
	MinLotSize    Quantity
	ContractType  string
}

// OrderBook represents the current state of an order book.
// Contains bids (buy orders), asks (sell orders), and metadata.
type OrderBook struct {
	Instrument     *Instrument // Pointer to Instrument details
	Asks           map[Price]Quantity // Map of price to quantity for asks
	Bids           map[Price]Quantity // Map of price to quantity for bids
	LastUpdateTime uint64 // Timestamp of the last update
	Sequence       uint64 // Sequence number for updates (important for state consistency)
	mu             sync.RWMutex // Mutex for concurrent read/write access to the maps
}

// Order represents a trading order (placeholder structure).
// This would be used when you decide to place an order on an exchange.
type Order struct {
	Instrument *Instrument // Pointer to the instrument being traded
	Side       string      // "buy" or "sell"
	Quantity   Quantity    // Quantity of the instrument to trade
	Price      Price       // Price for limit orders
	OrderType  string      // "limit", "market", etc.
	// Add more fields as needed for your target exchange's API (e.g., ClientOrderID, TimeInForce)
}

// OrderBookWithVWAP is a struct to hold the OrderBook and its calculated VWAP.
// The processor will output this type.
type OrderBookWithVWAP struct {
	OrderBook *OrderBook
	VWAP      Price
}

