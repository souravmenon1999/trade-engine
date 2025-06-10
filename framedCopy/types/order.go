package types

import (
	
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

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
	AmendType       AmendType
	NewPrice        *float64
	NewQty          *float64
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
		case AmendTypePriceQty:
			if update.NewPrice != nil && update.NewQty != nil {
				o.UpdatePrice(*update.NewPrice)
				o.UpdateQuantity(*update.NewQty)
			}
		case AmendTypePrice:
			if update.NewPrice != nil {
				o.UpdatePrice(*update.NewPrice)
			}
		case AmendTypeQty:
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



// NewLimitOrder creates a new limit order with the given parameters.
func NewLimitOrder(instrument *Instrument, side Side, exchangeID ExchangeID, price float64, quantity float64) *Order {
    order := &Order{
        ClientOrderID: uuid.New(),
        Instrument:    instrument,
        Side:          side,
        OrderType:     OrderTypeLimit,
        ExchangeID:    exchangeID,
        TimeInForce:   TimeInForceGTC,
        Price:         atomic.Int64{},
        Quantity:      atomic.Int64{},
        CreatedAt:     atomic.Int64{},
        UpdatedAt:     atomic.Int64{},
    }
    order.Price.Store(int64(price * SCALE_FACTOR_F64))
    order.Quantity.Store(int64(quantity * SCALE_FACTOR_F64))
    now := time.Now().UnixMilli()
    order.CreatedAt.Store(now)
    order.UpdatedAt.Store(now)
    return order
}

// NewMarketOrder creates a new market order with the given parameters.
func NewMarketOrder(instrument *Instrument, side Side, exchangeID ExchangeID, quantity float64) *Order {
    order := &Order{
        ClientOrderID: uuid.New(),
        Instrument:    instrument,
        Side:          side,
        OrderType:     OrderTypeMarket,
        ExchangeID:    exchangeID,
        TimeInForce:   TimeInForceIOC,
        Price:         atomic.Int64{}, // Price is not applicable
        Quantity:      atomic.Int64{},
        CreatedAt:     atomic.Int64{},
        UpdatedAt:     atomic.Int64{},
    }
    order.Quantity.Store(int64(quantity * SCALE_FACTOR_F64))
    now := time.Now().UnixMilli()
    order.CreatedAt.Store(now)
    order.UpdatedAt.Store(now)
    return order
}