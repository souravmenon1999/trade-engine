// internal/types/types.go
package types

import (
	"fmt"
	"sync/atomic"
)

// --- Enums using iota ---

type Side uint8

const (
	Buy Side = iota
	Sell
)

func (s Side) String() string {
	switch s {
	case Buy:
		return "Buy"
	case Sell:
		return "Sell"
	default:
		return fmt.Sprintf("UnknownSide(%d)", s)
	}
}

type ExchangeType uint8

const (
	ExchangeUnknown ExchangeType = iota
	ExchangeBybit
	ExchangeInjective
	// Add other exchanges here
)

func (e ExchangeType) String() string {
	switch e {
	case ExchangeBybit:
		return "Bybit"
	case ExchangeInjective:
		return "Injective"
	default:
		return fmt.Sprintf("UnknownExchange(%d)", e)
	}
}

type Currency string

const (
	CurrencyETH  Currency = "ETH"
	CurrencyUSDT Currency = "USDT"
	// Add other currencies here
)

type ContractType uint8

const (
	ContractTypeUnknown ContractType = iota
	ContractTypePerpetual
	ContractTypeFutures
	// Add other contract types here
)

// --- Standardized Structures ---

type Instrument struct {
	Symbol        string       // e.g., "ETHUSDT"
	BaseCurrency  Currency     // e.g., "ETH"
	QuoteCurrency Currency     // e.g., "USDT"
	MinLotSize    atomic.Uint64 // Minimum quantity scaled by 1e6
	ContractType  ContractType // e.g., Perpetual
}

// Order represents a standardized trading order.
type Order struct {
	Instrument *Instrument  // The instrument being traded
	Side       Side         // Enum: Buy/Sell
	Quantity   uint64       // Scaled by 1e6 (0.01 → 10_000)
	Price      uint64       // Scaled by 1e6 (1.0 → 1_000_000)
	Exchange   ExchangeType // The target exchange for the order
	// Add other fields as needed (e.g., ClientOrderID, OrderType, TimeInForce)
}

func (o Order) String() string {
	return fmt.Sprintf("%s %s %s %d@%d on %s",
		o.Side, o.Instrument.Symbol, "Qty", o.Quantity, o.Price, o.Exchange)
}

// --- Standardized Errors ---


type ErrorCode int

const (
	ErrUnknown ErrorCode = iota
	ErrConfigLoading
	ErrInvalidSequence
	ErrInsufficientLiquidity // Maybe from Orderbook
	ErrOrderRejected         // From Exchange API
	ErrConnectionFailed      // WebSocket or GRPC
	ErrOrderSubmissionFailed // Generic submission error
	ErrPriceProcessingFailed // Error during quote calculation
	// Add more specific codes as needed
)

// TradingError standardizes application errors.
type TradingError struct {
	Code    ErrorCode // Standardized code
	Message string    // Human-readable message
	Wrapped error     // Original error, if any
}

func (e TradingError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap provides compatibility with errors.Unwrap
func (e TradingError) Unwrap() error {
	return e.Wrapped
}