package types

import "sync"

type Price int64
type Quantity int64

type Instrument struct {
	BaseCurrency  string
	QuoteCurrency string
	MinLotSize    Quantity
	ContractType  string
}

type OrderBook struct {
	Instrument     *Instrument
	Asks           map[Price]Quantity
	Bids           map[Price]Quantity
	LastUpdateTime uint64
	Sequence       uint64
	Mu             sync.RWMutex
}

type OrderBookWithVWAP struct {
	OrderBook *OrderBook
	VWAP      Price
}