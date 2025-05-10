// internal/types/types.go - Using provided code
package types

import (
	"errors"
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

// Instrument represents a tradable asset.
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

// ErrorCode defines standard error reasons.
type ErrorCode int

const (
	ErrUnknown ErrorCode = iota
	ErrConfigLoading
	ErrInvalidSequence
	ErrInsufficientLiquidity // ErrorCode for insufficient liquidity condition
	ErrOrderRejected         // From Exchange API
	ErrConnectionFailed      // WebSocket or GRPC
	ErrOrderSubmissionFailed // Generic submission error
	ErrPriceProcessingFailed // Error during quote calculation
	// Add more specific codes as needed
)

// Define base errors that can be wrapped by TradingError.
// These are private to the package and provide consistent underlying errors.
var (
	ErrBaseInsufficientLiquidity = errors.New("insufficient liquidity") // Base error for this condition
	// Add other base errors if needed, e.g., errBaseConnectionFailed = errors.New("connection failed")
)


// TradingError standardizes application errors.
type TradingError struct {
	Code    ErrorCode // Standardized code
	Message string    // Human-readable message (user-friendly context)
	Wrapped error     // Original error, if any (for debugging)
}

func (e TradingError) Error() string {
	if e.Wrapped != nil {
		// Include the specific message provided when the TradingError was created
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Wrapped)
	}
	// If no specific message is provided, use a default based on code or just the code
	// For now, we assume Message will always be provided when creating TradingError
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap provides compatibility with errors.Unwrap
func (e TradingError) Unwrap() error {
	return e.Wrapped
}