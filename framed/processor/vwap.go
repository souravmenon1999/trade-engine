// orderbook/orderbook.go
package priceproccessor

import (
	"your_module_path/types" // Replace with your actual module path
)

// CalculateVWAP calculates the Volume Weighted Average Price for an OrderBook.
// It takes a pointer to the types.OrderBook struct.
func CalculateVWAP(ob *types.OrderBook) types.Price {
	// Use RLock for reading data safely from the OrderBook struct's mutex
	ob.mu.RLock()
	defer ob.mu.RUnlock() // Ensure unlock happens when the function returns

	var sumPQ, sumQ int64

	// Calculate sum of (Price * Quantity) and sum of Quantity for Bids
	for price, qty := range ob.Bids {
		sumPQ += int64(price) * int64(qty)
		sumQ += int64(qty)
	}

	// Calculate sum of (Price * Quantity) and sum of Quantity for Asks
	for price, qty := range ob.Asks {
		sumPQ += int64(price) * int64(qty)
		sumQ += int64(qty)
	}

	// Avoid division by zero if there are no bids or asks
	if sumQ == 0 {
		return 0
	}

	// Calculate VWAP: (Sum of Price * Quantity) / (Sum of Quantity)
	// The result is an int64 which is then cast back to types.Price
	return types.Price(sumPQ / sumQ)
}

