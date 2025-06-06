package types

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	SCALE_FACTOR     = 1_000_000
	SCALE_FACTOR_F64 = 1_000_000.0
	UNSET_VALUE      = -1
)

// Price is a struct embedding atomic.Int64 for thread-safe price values.
type Price struct {
	atomic.Int64
}

// Quantity is a struct embedding atomic.Int64 for thread-safe quantity values.
type Quantity struct {
	atomic.Int64
}

type ExchangeID string

const (
	ExchangeIDBybit     ExchangeID = "bybit"
	ExchangeIDInjective ExchangeID = "injective"
)

// NewPrice creates a new Price with an initial value and returns a pointer.
func NewPrice(val int64) *Price {
	p := &Price{}
	p.Store(val)
	return p
}

// NewQuantity creates a new Quantity with an initial value and returns a pointer.
func NewQuantity(val float64) *Quantity {
	q := &Quantity{}
	q.Store(int64(val * SCALE_FACTOR))
	return q
}

func (q *Quantity) ToFloat64() float64 {
	return float64(q.Load()) / SCALE_FACTOR
}

// Instrument represents a trading instrument.
type Instrument struct {
	Symbol        string
	BaseCurrency  string
	QuoteCurrency string
	MinLotSize    *Quantity
	ContractType  string
}

// OrderBook represents the state of an order book with atomic fields.
type OrderBook struct {
	Instrument     *Instrument
	Asks           sync.Map
	Bids           sync.Map
	LastUpdateTime atomic.Int64
	Sequence       atomic.Int64
	Exchange       *ExchangeID
}

// OrderBookWithVWAP combines an order book with its VWAP.
type OrderBookWithVWAP struct {
	OrderBook *OrderBook
	VWAP      *Price
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

// Order represents a trading order.
type Order struct {
	ClientOrderID   uuid.UUID   // Unique client-generated ID
	Instrument      *Instrument // Trading instrument
	Side            Side        // Buy or Sell
	OrderType       OrderType   // Limit or Market
	ExchangeID      ExchangeID  // Bybit or Injective
	TimeInForce     TimeInForce // GTC or IOC
	ExchangeOrderID string      // Order ID assigned by the exchange
	// Thread-safe fields
	Status         atomic.Int64 // Order status
	Quantity       atomic.Int64 // Total quantity (scaled)
	FilledQuantity atomic.Int64 // Filled quantity (scaled)
	Price          atomic.Int64 // Price (scaled)
	CreatedAt      atomic.Int64 // Creation timestamp (ms)
	UpdatedAt      atomic.Int64 // Last update timestamp (ms)
}

// GetStatus returns the current order status.
func (o *Order) GetStatus() OrderStatus {
	return OrderStatus(o.Status.Load())
}

// GetQuantity returns the quantity as float64.
func (o *Order) GetQuantity() float64 {
	return float64(o.Quantity.Load()) / SCALE_FACTOR_F64
}

// GetFilledQuantity returns the filled quantity as float64.
func (o *Order) GetFilledQuantity() float64 {
	return float64(o.FilledQuantity.Load()) / SCALE_FACTOR_F64
}

// GetPrice returns the price as float64.
func (o *Order) GetPrice() float64 {
	return float64(o.Price.Load()) / SCALE_FACTOR_F64
}

// UpdateStatus updates the order status.
func (o *Order) UpdateStatus(status OrderStatus) {
	o.Status.Store(int64(status))
	o.UpdatedAt.Store(time.Now().UnixMilli())
}

// UpdateFilledQuantity updates the filled quantity.
func (o *Order) UpdateFilledQuantity(fillQty float64) {
	fill := int64(fillQty * SCALE_FACTOR_F64)
	o.FilledQuantity.Add(fill)
	o.UpdatedAt.Store(time.Now().UnixMilli())
}

// UpdateQuantity updates the total quantity.
func (o *Order) UpdateQuantity(qty float64) {
	o.Quantity.Store(int64(qty * SCALE_FACTOR_F64))
	o.UpdatedAt.Store(time.Now().UnixMilli())
}

// UpdatePrice updates the price.
func (o *Order) UpdatePrice(price float64) {
	o.Price.Store(int64(price * SCALE_FACTOR_F64))
	o.UpdatedAt.Store(time.Now().UnixMilli())
}

// ApplyUpdate applies an update to the order.
func (o *Order) ApplyUpdate(update *OrderUpdate) {
	if !update.Success {
		return
	}

	newTimestamp := update.UpdatedAt
	isFill := update.UpdateType == OrderUpdateTypeFill
	noExchangeID := o.ExchangeOrderID == ""
	shouldApply := newTimestamp > o.UpdatedAt.Load() || isFill || noExchangeID
	if !shouldApply {
		return
	}

	o.UpdatedAt.Store(newTimestamp)

	switch update.UpdateType {
	case OrderUpdateTypeCreated:
		if update.ExchangeOrderID != nil {
			o.ExchangeOrderID = *update.ExchangeOrderID
			if o.GetStatus() == OrderStatusSubmitted {
				o.UpdateStatus(OrderStatusOpen)
			}
		}
	case OrderUpdateTypeAmended:
		switch update.AmendType {
		case "price_qty":
			if update.NewPrice != nil && update.NewQty != nil {
				o.UpdatePrice(*update.NewPrice)
				o.UpdateQuantity(*update.NewQty)
			}
		case "price":
			if update.NewPrice != nil {
				o.UpdatePrice(*update.NewPrice)
			}
		case "qty":
			if update.NewQty != nil {
				o.UpdateQuantity(*update.NewQty)
			}
		}
	case OrderUpdateTypeCanceled:
		currentStatus := o.GetStatus()
		if currentStatus != OrderStatusFilled && currentStatus != OrderStatusRejected {
			o.UpdateStatus(OrderStatusCancelled)
		}
	case OrderUpdateTypeFill:
		if update.FillQty != nil {
			o.UpdateFilledQuantity(*update.FillQty)
			newFilled := o.FilledQuantity.Load()
			currentQty := o.Quantity.Load()
			if newFilled >= currentQty {
				o.UpdateStatus(OrderStatusFilled)
				if newFilled > currentQty {
					o.FilledQuantity.Store(currentQty)
				}
			} else {
				o.UpdateStatus(OrderStatusPartiallyFilled)
			}
		}
	case OrderUpdateTypeRejected:
		o.UpdateStatus(OrderStatusRejected)
	}
}

// OrderUpdate represents an update to an order.
type OrderUpdate struct {
	Success         bool
	UpdateType      OrderUpdateType
	Status          OrderStatus
	ErrorMessage    *string
	RequestID       *string
	ExchangeOrderID *string
	FillQty         *float64
	FillPrice       *float64
	UpdatedAt       int64
	IsMaker         bool
	AmendType       string
	NewPrice        *float64
	NewQty          *float64
}