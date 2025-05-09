// internal/types/orderbook.go
package types

import (
	//"errors" // errors is now only used in types.go for base errors
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// PriceLevel represents aggregated orders at a specific price.
type PriceLevel struct {
	Price    uint64        // Scaled price (e.g., 1.0 -> 1,000,000)
	Quantity atomic.Uint64 // Total quantity at this price level (scaled)
	// Add other relevant fields if needed (e.g., number of orders)
}

// Orderbook represents the current state of buy and sell orders for an instrument.
// Uses sync.Map for concurrent access without explicit locks on the map itself.
type Orderbook struct {
	Instrument *Instrument // The instrument this orderbook is for
	Bids       *sync.Map   // Map[uint64 (scaled price)]*PriceLevel
	Asks       *sync.Map   // Map[uint64 (scaled price)]*PriceLevel

	SeqNumber atomic.Uint64 // Sequence number for update validation (exchange-specific)
	Timestamp atomic.Int64  // Timestamp of the last update (Unix Nano)

	// Potentially add mutex if complex multi-field updates are needed,
	// but sync.Map methods handle individual level updates concurrently.
}

// NewOrderbook creates and initializes a new Orderbook.
func NewOrderbook(instrument *Instrument) *Orderbook {
	return &Orderbook{
		Instrument: instrument,
		Bids:       &sync.Map{},
		Asks:       &sync.Map{},
	}
}

// BestBid finds the highest bid price in the orderbook.
// Returns the scaled price (uint64) and true if found, 0 and false otherwise.
func (ob *Orderbook) BestBid() (uint64, bool) {
	var bestPrice uint64
	found := false

	ob.Bids.Range(func(key, value any) bool {
		price := key.(uint64)
		if price > bestPrice {
			bestPrice = price
			found = true
		}
		return true // continue iteration
	})

	return bestPrice, found
}

// BestAsk finds the lowest ask price in the orderbook.
// Returns the scaled price (uint64) and true if found, 0 and false otherwise.
func (ob *Orderbook) BestAsk() (uint64, bool) {
	var bestPrice uint64 // Initial value (0) is higher than any valid price > 0
	found := false

	ob.Asks.Range(func(key, value any) bool {
		price := key.(uint64)
		// For asks, we want the *lowest* price
		if !found || price < bestPrice {
			bestPrice = price
			found = true
		}
		return true // continue iteration
	})

	return bestPrice, found
}

// REMOVE THIS LINE: var ErrInsufficientLiquidity = errors.New("insufficient liquidity for mid-price calculation")


// MidPrice calculates the mid-price (average of best bid and best ask).
// Returns the mid-price as float64 (unscaled) and an error if best bid/ask are not available.
func (ob *Orderbook) MidPrice() (float64, error) { // Changed float66 to float64
	bestBid, bidFound := ob.BestBid()
	bestAsk, askFound := ob.BestAsk()

	if !bidFound || !askFound {
		// Return TradingError wrapping the base error
		return 0.0, TradingError{
			Code:    ErrInsufficientLiquidity,
			Message: "Best bid or ask not found",
			Wrapped: ErrBaseInsufficientLiquidity, // Make sure ErrBaseInsufficientLiquidity is defined and exported or accessible
		}
	}

	// Ensure both best bid and best ask are non-zero (valid prices)
	if bestBid == 0 || bestAsk == 0 {
         // Return TradingError wrapping the base error
         return 0.0, TradingError{
             Code:    ErrInsufficientLiquidity,
             Message: "Best bid or ask is zero",
             Wrapped: ErrBaseInsufficientLiquidity,
         }
    }


	// Prices are uint64 scaled by 1e6. Convert to float64 for calculation.
	mid := (float64(bestBid) + float64(bestAsk)) / 2.0

	// Convert back to unscaled float64 for the return value
	return mid / 1e6, nil
}

// GetBids returns bids sorted by price descending. Useful for display/debugging.
func (ob *Orderbook) GetBids() []*PriceLevel {
	var levels []*PriceLevel
	ob.Bids.Range(func(key, value any) bool {
		levels = append(levels, value.(*PriceLevel))
		return true
	})
	sort.SliceStable(levels, func(i, j int) bool {
		return levels[i].Price > levels[j].Price // Sort descending by price
	})
	return levels
}

// GetAsks returns asks sorted by price ascending. Useful for display/debugging.
func (ob *Orderbook) GetAsks() []*PriceLevel {
	var levels []*PriceLevel
	ob.Asks.Range(func(key, value any) bool {
		levels = append(levels, value.(*PriceLevel))
		return true
	})
	sort.SliceStable(levels, func(i, j int) bool {
		return levels[i].Price < levels[j].Price // Sort ascending by price
	})
	return levels
}

// Snapshot creates a deep copy of the orderbook for a consistent view.
// Note: For large orderbooks, this can be resource-intensive.
// Consider if a snapshot is truly needed vs. processing updates sequentially
// or accessing atomic fields directly for specific values like BestBid/Ask.
func (ob *Orderbook) Snapshot() *Orderbook {
    snapshot := &Orderbook{
        Instrument: ob.Instrument, // Instrument struct is small, safe to copy reference/value
        Bids:       &sync.Map{},
        Asks:       &sync.Map{},
    }

    // Copy bid levels
    ob.Bids.Range(func(key, value any) bool {
        level := value.(*PriceLevel)
        // Create a new PriceLevel and copy atomic value
        newLevel := &PriceLevel{
            Price: level.Price,
        }
        newLevel.Quantity.Store(level.Quantity.Load())
        snapshot.Bids.Store(key, newLevel)
        return true
    })

    // Copy ask levels
    ob.Asks.Range(func(key, value any) bool {
        level := value.(*PriceLevel)
        // Create a new PriceLevel and copy atomic value
        newLevel := &PriceLevel{
            Price: level.Price,
        }
        newLevel.Quantity.Store(level.Quantity.Load())
        snapshot.Asks.Store(key, newLevel)
        return true
    })

    // Copy atomic values
    snapshot.SeqNumber.Store(ob.SeqNumber.Load())
    snapshot.Timestamp.Store(ob.Timestamp.Load())

    return snapshot
}

// UpdateTimestamp updates the internal timestamp to the current time.
func (ob *Orderbook) UpdateTimestamp() {
    ob.Timestamp.Store(time.Now().UnixNano())
}

// UpdateSequenceNumber updates the internal sequence number.
func (ob *Orderbook) UpdateSequenceNumber(seq uint64) {
    ob.SeqNumber.Store(seq)
}