package types

import(
	"sync"
	"sync/atomic"
	"time"

)




type Price struct {
	atomic.Int64
}

// Quantity is a struct embedding atomic.Int64 for thread-safe quantity values.
type Quantity struct {
	atomic.Int64
}

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


func scale(value float64) int64 { return int64(value * SCALE_FACTOR_F64) }
func Unscale(value int64) float64 { return float64(value) / SCALE_FACTOR_F64 }


type OrderBookUpdate struct {
	Price      int64  
	Quantity   int64  
	Side       Side   
	OrderCount uint32
	Sequence   int64 
	
	
}

// PriceLevel represents a single price level in the order book.
type PriceLevel struct {
	Quantity   int64         // Scaled quantity
	OrderCount uint32        // Number of orders at this level
	mu         sync.RWMutex  // Mutex for thread-safe updates to this level
}

// OrderBook represents the order book for an instrument on an exchange.
type OrderBook struct {
	Instrument     *Instrument
	Bids           sync.Map      // Map of int64 (scaled price) to *PriceLevel
	Asks           sync.Map      // Map of int64 (scaled price) to *PriceLevel
	LastUpdateTime atomic.Int64  // Last update timestamp
	Sequence       atomic.Int64  // Sequence number for ordering updates
	Exchange       *ExchangeID
}

// NewOrderBook creates a new OrderBook instance.
func NewOrderBook(instrument *Instrument, exchange *ExchangeID) *OrderBook {
	return &OrderBook{
		Instrument: instrument,
		Exchange:   exchange,
	}
}

// IsRemoval checks if this update signifies the removal of a price level.
func (u *OrderBookUpdate) IsRemoval() bool {
	return u.Quantity == 0
}

// NewOrderBookUpdate creates a new OrderBookUpdate with scaled price and quantity.
func NewOrderBookUpdate(price, quantity float64, side Side, orderCount uint32, sequence int64) *OrderBookUpdate {
	return &OrderBookUpdate{
		Price:      scale(price),
		Quantity:   scale(quantity),
		Side:       side,
		OrderCount: orderCount,
		Sequence:   sequence,
	}
}

func BidUpdate(price, quantity float64, orderCount uint32, sequence int64) *OrderBookUpdate {
	return NewOrderBookUpdate(price, quantity, SideBuy, orderCount, sequence)
}

func AskUpdate(price, quantity float64, orderCount uint32, sequence int64) *OrderBookUpdate {
	return NewOrderBookUpdate(price, quantity, SideSell, orderCount, sequence)
}

// SetLastUpdateTime sets the last update timestamp atomically.
func (ob *OrderBook) SetLastUpdateTime(ts int64) {
	ob.LastUpdateTime.Store(ts)
}

// SetSequence sets the sequence number atomically.
func (ob *OrderBook) SetSequence(seq int64) {
	ob.Sequence.Store(seq)
}

// ApplyUpdate applies a single order book update.
func (ob *OrderBook) ApplyUpdate(update *OrderBookUpdate) {
	var targetMap *sync.Map
	if update.Side == SideBuy {
		targetMap = &ob.Bids
	} else {
		targetMap = &ob.Asks
	}

	priceKey := update.Price

	// Load or create the price level
	plInterface, _ := targetMap.LoadOrStore(priceKey, &PriceLevel{})
	pl := plInterface.(*PriceLevel)

	// Lock only this price level for writing
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if update.IsRemoval() {
		targetMap.Delete(priceKey)
	} else {
		pl.Quantity = update.Quantity
		pl.OrderCount = update.OrderCount
	}

	// Update metadata
	ob.LastUpdateTime.Store(time.Now().UnixMilli())
}

// GetMidPrice calculates the mid-price from the best bid and ask.
func (ob *OrderBook) GetMidPrice() (float64, bool) {
	var bestBid, bestAsk int64
	var hasBid, hasAsk bool

	ob.Bids.Range(func(key, value interface{}) bool {
		price := key.(int64)
		if price > bestBid {
			bestBid = price
			hasBid = true
		}
		return true
	})

	ob.Asks.Range(func(key, value interface{}) bool {
		price := key.(int64)
		if !hasAsk || price < bestAsk {
			bestAsk = price
			hasAsk = true
		}
		return true
	})

	if !hasBid || !hasAsk {
		return 0, false
	}

	// Assuming unscale converts int64 price to float64 (implement as needed)
	midPrice := (Unscale(bestBid) + Unscale(bestAsk)) / 2
	return midPrice, true
}

// GetSequence returns the current sequence number.
func (ob *OrderBook) GetSequence() (int64, bool) {
	return ob.Sequence.Load(), true
}



