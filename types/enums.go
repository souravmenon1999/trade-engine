package types

import "strconv"

// OrderStatus represents the status of an order.
type OrderStatus int

const (
	OrderStatusSubmitted     OrderStatus = 0
	OrderStatusOpen          OrderStatus = 1
	OrderStatusPartiallyFilled OrderStatus = 2
	OrderStatusFilled        OrderStatus = 3
	OrderStatusCancelled     OrderStatus = 4
	OrderStatusRejected      OrderStatus = 5
	OrderStatusUnknown       OrderStatus = -1
)

func (os OrderStatus) String() string {
	switch os {
	case OrderStatusSubmitted:
		return "Submitted"
	case OrderStatusOpen:
		return "Open"
	case OrderStatusPartiallyFilled:
		return "PartiallyFilled"
	case OrderStatusFilled:
		return "Filled"
	case OrderStatusCancelled:
		return "Cancelled"
	case OrderStatusRejected:
		return "Rejected"
	case OrderStatusUnknown:
		return "Unknown"
	default:
		return "OrderStatus(" + strconv.Itoa(int(os)) + ")"
	}
}

// OrderType represents the types of orders supported by the system.
type OrderType int

const (
	OrderTypeMarket    OrderType = 0
	OrderTypeLimit     OrderType = 1
	OrderTypeStopMarket OrderType = 2
	OrderTypeStopLimit  OrderType = 3
)

func (ot OrderType) String() string {
	switch ot {
	case OrderTypeMarket:
		return "Market"
	case OrderTypeLimit:
		return "Limit"
	case OrderTypeStopMarket:
		return "StopMarket"
	case OrderTypeStopLimit:
		return "StopLimit"
	default:
		return "UnknownOrderType"
	}
}

// Side represents the trading side (buy or sell).
type Side int

const (
	SideBuy  Side = 0
	SideSell Side = 1
)

func (s Side) String() string {
	switch s {
	case SideBuy:
		return "Buy"
	case SideSell:
		return "Sell"
	default:
		return "UnknownSide"
	}
}

func (s Side) Opposite() Side {
	if s == SideBuy {
		return SideSell
	}
	return SideBuy
}

// TimeInForce represents the time in force for orders.
type TimeInForce int

const (
	TimeInForceGTC      TimeInForce = 0
	TimeInForceIOC      TimeInForce = 1
	TimeInForceFOK      TimeInForce = 2
	TimeInForceGTD      TimeInForce = 3
	TimeInForcePostOnly TimeInForce = 4
)

func (tif TimeInForce) String() string {
	switch tif {
	case TimeInForceGTC:
		return "GTC"
	case TimeInForceIOC:
		return "IOC"
	case TimeInForceFOK:
		return "FOK"
	case TimeInForceGTD:
		return "GTD"
	case TimeInForcePostOnly:
		return "POST_ONLY"
	default:
		return "UnknownTimeInForce"
	}
}

// OrderUpdateType represents the type of order update.
type OrderUpdateType string

const (
	OrderUpdateTypeCreated  OrderUpdateType = "Created"
	OrderUpdateTypeAmended  OrderUpdateType = "Amended"
	OrderUpdateTypeCanceled OrderUpdateType = "Canceled"
	OrderUpdateTypeFill     OrderUpdateType = "Fill"
	OrderUpdateTypeRejected OrderUpdateType = "Rejected"
	OrderUpdateTypeOther    OrderUpdateType = "Other"
)

func (out OrderUpdateType) String() string {
	switch out {
	case OrderUpdateTypeCreated:
		return "Created"
	case OrderUpdateTypeAmended:
		return "Amended"
	case OrderUpdateTypeCanceled:
		return "Canceled"
	case OrderUpdateTypeFill:
		return "Fill"
	case OrderUpdateTypeRejected:
		return "Rejected"
	case OrderUpdateTypeOther:
		return "Other"
	default:
		return "UnknownOrderUpdateType"
	}
}

// AmendType represents the type of amendment for an amended order update.
type AmendType int

const (
	AmendTypePriceQty AmendType = 0
	AmendTypePrice    AmendType = 1
	AmendTypeQty      AmendType = 2
)

func (at AmendType) String() string {
	switch at {
	case AmendTypePriceQty:
		return "PriceQty"
	case AmendTypePrice:
		return "Price"
	case AmendTypeQty:
		return "Qty"
	default:
		return "UnknownAmendType"
	}
}

// Constants
