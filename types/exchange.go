package types

type ExchangeType string

const (
	ExchangeTypeBybit   ExchangeType = "Bybit"
	ExchangeTypeBinance ExchangeType = "Binance"
	ExchangeTypeDrift   ExchangeType = "Drift"
	// For "Other", any string value can be used.
)


func (et ExchangeType) String() string {
	return string(et)
}

// Exchange represents a trading exchange/venue.
type Exchange struct {
	// The type of exchange
	ExchangeType ExchangeType `json:"exchange_type"`
	// Unique identifier for the exchange
	ID string `json:"id"`
	// Display name of the exchange
	Name string `json:"name"`
	// Whether the exchange is currently active
	Active bool `json:"active"`
}

// NewBybitExchange creates a Bybit exchange instance and returns a pointer to it.
func NewBybitExchange(active bool) *Exchange {
	return &Exchange{ // Use '&' to return a pointer to a new Exchange struct
		ExchangeType: ExchangeTypeBybit,
		ID:           "bybit",
		Name:         "Bybit",
		Active:       active,
	}
}

// NewBinanceExchange creates a Binance exchange instance and returns a pointer to it.
func NewBinanceExchange(active bool) *Exchange {
	return &Exchange{ // Use '&' to return a pointer to a new Exchange struct
		ExchangeType: ExchangeTypeBinance,
		ID:           "binance",
		Name:         "Binance",
		Active:       active,
	}
}

// NewDriftExchange creates a Drift exchange instance and returns a pointer to it.
func NewDriftExchange(active bool) *Exchange {
	return &Exchange{ // Use '&' to return a pointer to a new Exchange struct
		ExchangeType: ExchangeTypeDrift,
		ID:           "drift",
		Name:         "Drift",
		Active:       active,
	}
}

// String implements the fmt.Stringer interface for Exchange.
// For Stringer, a value receiver is typically used as it's a read-only operation.
func (e Exchange) String() string {
	return e.Name
}