package types

import (
	
	"strconv" // For converting int to string in OrderStatus String() method
)

// Side represents the trading side (buy or sell)
type Side int

const (
	Buy Side = iota // 0
	Sell          // 1
)

// String implements the fmt.Stringer interface for Side.
// It uses a pointer receiver as requested. Note that for simple value types,
// a value receiver is often more idiomatic and performant for read-only methods.
func (s *Side) String() string {
	if s == nil {
		return "nil Side" // Handle nil pointer gracefully
	}
	switch *s { // Dereference the pointer to get the underlying value
	case Buy:
		return "Buy"
	case Sell:
		return "Sell"
	default:
		return "UnknownSide" // Handle unexpected values
	}
}

// Opposite returns the opposite trading side.
// It uses a pointer receiver as requested.
func (s *Side) Opposite() Side {
	if s == nil {
		// Return a default or handle error for nil pointer
		return Buy // Or panic, depending on desired error handling
	}
	if *s == Buy { // Dereference the pointer
		return Sell
	}
	return Buy
}

// OrderStatus represents the status of an order.
type OrderStatus int

const (
	Submitted     OrderStatus = 0
	Open          OrderStatus = 1
	PartiallyFilled OrderStatus = 2
	Filled        OrderStatus = 3
	Cancelled     OrderStatus = 4
	Rejected      OrderStatus = 5
	Unknown       OrderStatus = -1 // Using -1 for unknown, similar to Rust
)

// String implements the fmt.Stringer interface for OrderStatus.
// It uses a pointer receiver as requested.
func (os *OrderStatus) String() string {
	if os == nil {
		return "nil OrderStatus"
	}
	switch *os { // Dereference the pointer
	case Submitted:
		return "Submitted"
	case Open:
		return "Open"
	case PartiallyFilled:
		return "PartiallyFilled"
	case Filled:
		return "Filled"
	case Cancelled:
		return "Cancelled"
	case Rejected:
		return "Rejected"
	case Unknown:
		return "Unknown"
	default:
		return "OrderStatus(" + strconv.Itoa(int(*os)) + ")" // Fallback for unexpected values
	}
}

// OrderType represents the types of orders supported by the system.
type OrderType int

const (
	Market OrderType = iota // 0: Market order (executes immediately at best available price)
	Limit                 // 1: Limit order (executes only at specified price or better)
	StopMarket            // 2: Stop market order (becomes market order when price hits trigger)
	StopLimit             // 3: Stop limit order (becomes limit order when price hits trigger)
)

// String implements the fmt.Stringer interface for OrderType.
// It uses a pointer receiver as requested.
func (ot *OrderType) String() string {
	if ot == nil {
		return "nil OrderType"
	}
	switch *ot { // Dereference the pointer
	case Market:
		return "Market"
	case Limit:
		return "Limit"
	case StopMarket:
		return "StopMarket"
	case StopLimit:
		return "StopLimit"
	default:
		return "UnknownOrderType"
	}
}

// ContractType represents the types of contracts/instruments that can be traded.
type ContractType int

const (
	Spot      ContractType = iota // 0: Spot trading (immediate delivery)
	Futures                       // 1: Futures contracts
	Perpetual                     // 2: Perpetual swap contracts
	Option                        // 3: Options contracts
)

// String implements the fmt.Stringer interface for ContractType.
// It uses a pointer receiver as requested.
func (ct *ContractType) String() string {
	if ct == nil {
		return "nil ContractType"
	}
	switch *ct { // Dereference the pointer
	case Spot:
		return "Spot"
	case Futures:
		return "Futures"
	case Perpetual:
		return "Perpetual"
	case Option:
		return "Option"
	default:
		return "UnknownContractType"
	}
}

// TimeInForce represents the time in force for orders.
type TimeInForce int

const (
	GTC TimeInForce = iota // 0: Good Till Cancelled
	IOC                     // 1: Immediate or Cancel
	FOK                     // 2: Fill or Kill
	GTD                     // 3: Good Till Date
	POST_ONLY               // 4: Post Only
)

// String implements the fmt.Stringer interface for TimeInForce.
// It uses a pointer receiver as requested.
func (tif *TimeInForce) String() string {
	if tif == nil {
		return "nil TimeInForce"
	}
	switch *tif { // Dereference the pointer
	case GTC:
		return "GTC"
	case IOC:
		return "IOC"
	case FOK:
		return "FOK"
	case GTD:
		return "GTD"
	case POST_ONLY:
		return "POST_ONLY"
	default:
		return "UnknownTimeInForce"
	}
}