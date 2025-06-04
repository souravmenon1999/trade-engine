package types

import (
	
	"strconv" 

)
type Side int

const (
	Buy Side = iota 
	Sell         
)


func (s *Side) String() string {
	if s == nil {
		return "nil Side" 
	}
	switch *s { 
	case Buy:
		return "Buy"
	case Sell:
		return "Sell"
	default:
		return "UnknownSide" 
	}
}

// Opposite returns the opposite trading side.

func (s *Side) Opposite() Side {
	if s == nil {
		
		return Buy 
	}
	if *s == Buy { 
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
	Unknown       OrderStatus = -1 
)



func (os *OrderStatus) String() string {
	if os == nil {
		return "nil OrderStatus"
	}
	switch *os { 
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
		return "OrderStatus(" + strconv.Itoa(int(*os)) + ")" 
	}
}

// OrderType represents the types of orders supported by the system.
type OrderType int

const (
	Market OrderType = iota 
	Limit                 
	StopMarket           
	StopLimit            
)

// String implements the fmt.Stringer interface for OrderType.

func (ot *OrderType) String() string {
	if ot == nil {
		return "nil OrderType"
	}
	switch *ot {
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
	Spot      ContractType = iota 
	Futures                      
	Perpetual                    
	Option                       
)

// String implements the fmt.Stringer interface for ContractType.
func (ct *ContractType) String() string {
	if ct == nil {
		return "nil ContractType"
	}
	switch *ct { 
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