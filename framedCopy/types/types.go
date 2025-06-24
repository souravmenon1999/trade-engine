package types

import(
	"time"
)



const (
	SCALE_FACTOR     = 1_000_000
	SCALE_FACTOR_F64 = 1_000_000.0
	UNSET_VALUE      = -1
)

type ExchangeID string

const (
	ExchangeIDBybit     ExchangeID = "bybit"
	ExchangeIDInjective ExchangeID = "injective"
)





type ContractType string

const (
    ContractTypePerpetual ContractType = "Perpetual"
   
)

// Instrument represents a trading instrument.
type Instrument struct {
    Symbol        string
    BaseCurrency  string
    QuoteCurrency string
    MinLotSize    *Quantity
    ContractType  ContractType 
}



// Side defines the order side.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType defines the type of order.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// TimeInForce defines the order's time in force.
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC"
	TimeInForceIOC TimeInForce = "IOC"
)

// OrderStatus defines the possible states of an order.
type OrderStatus int64

const (
	OrderStatusSubmitted      OrderStatus = 0
	OrderStatusOpen           OrderStatus = 1
	OrderStatusPartiallyFilled OrderStatus = 2
	OrderStatusFilled         OrderStatus = 3
	OrderStatusCancelled      OrderStatus = 4
	OrderStatusRejected       OrderStatus = 5
	OrderStatusUnknown        OrderStatus = -1
)

// OrderUpdateType defines the type of order update.
type OrderUpdateType string

const (
	OrderUpdateTypeCreated  OrderUpdateType = "created"
	OrderUpdateTypeAmended  OrderUpdateType = "amended"
	OrderUpdateTypeCanceled OrderUpdateType = "canceled"
	OrderUpdateTypeFill     OrderUpdateType = "fill"
	OrderUpdateTypeRejected OrderUpdateType = "rejected"
	OrderUpdateTypeOther    OrderUpdateType = "other"
)

// AmendType defines the type of amendment for an amended order update.
type AmendType string

const (
	AmendTypePriceQty AmendType = "price_qty"
	AmendTypePrice    AmendType = "price"
	AmendTypeQty      AmendType = "qty"
)


type AccountUpdate struct {
    Exchange   *ExchangeID 
    AccountIM  float64  
    AccountMM  float64   
    
}

// Position represents a trading position
type Position struct {
    Exchange      *ExchangeID  // Pointer to exchange details
    Instrument    *Instrument // The trading pair/symbol
    Side          Side       // "Buy" or "Sell"
    Quantity      float64    // Position size
    EntryPrice    float64    // Average entry price
    UnrealizedPnL float64    // Unrealized profit/loss
    
}

// NewPerpetualInstrument creates a new instrument for perpetual contracts with a configurable min lot size.
func NewPerpetualInstrument(symbol, baseCurrency, quoteCurrency string, minLotSize float64) *Instrument {
    return &Instrument{
        Symbol:        symbol,
        BaseCurrency:  baseCurrency,
        QuoteCurrency: quoteCurrency,
        MinLotSize:    NewQuantity(minLotSize),
        ContractType:  ContractTypePerpetual,
    }
}

type FundingRate struct {
    Rate               float64       
    Interval           time.Duration 
    LastUpdated        int64         
    NextFundingTimestamp int64        
}
