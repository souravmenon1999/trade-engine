package types

import "fmt"

// TradingError represents an error in the trading system.
type TradingError struct {
	Kind    TradingErrorKind
	Message string
}

// TradingErrorKind defines the specific type of trading error.
type TradingErrorKind int

const (
	InvalidQuantityError     TradingErrorKind = iota
	InvalidPriceError
	InsufficientLiquidityError
	OrderError
	ExchangeError
	InstrumentError
	MissingDataError
)

// Error implements the error interface for TradingError.
func (e *TradingError) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind.String(), e.Message)
}

// String returns a string representation of the TradingErrorKind.
func (k TradingErrorKind) String() string {
	switch k {
	case InvalidQuantityError:
		return "Invalid quantity"
	case InvalidPriceError:
		return "Invalid price"
	case InsufficientLiquidityError:
		return "Insufficient liquidity"
	case OrderError:
		return "Order error"
	case ExchangeError:
		return "Exchange error"
	case InstrumentError:
		return "Instrument error"
	case MissingDataError:
		return "Missing data"
	default:
		return "Unknown trading error"
	}
}

// NewInvalidQuantityError creates a new InvalidQuantityError.
func NewInvalidQuantityError(message string) *TradingError {
	return &TradingError{Kind: InvalidQuantityError, Message: message}
}

// NewInvalidPriceError creates a new InvalidPriceError.
func NewInvalidPriceError(message string) *TradingError {
	return &TradingError{Kind: InvalidPriceError, Message: message}
}

// NewInsufficientLiquidityError creates a new InsufficientLiquidityError.
func NewInsufficientLiquidityError(message string) *TradingError {
	return &TradingError{Kind: InsufficientLiquidityError, Message: message}
}

// NewOrderError creates a new OrderError.
func NewOrderError(message string) *TradingError {
	return &TradingError{Kind: OrderError, Message: message}
}

// NewExchangeError creates a new ExchangeError.
func NewExchangeError(message string) *TradingError {
	return &TradingError{Kind: ExchangeError, Message: message}
}

// NewInstrumentError creates a new InstrumentError.
func NewInstrumentError(message string) *TradingError {
	return &TradingError{Kind: InstrumentError, Message: message}
}

// NewMissingDataError creates a new MissingDataError.
func NewMissingDataError(message string) *TradingError {
	return &TradingError{Kind: MissingDataError, Message: message}
}

