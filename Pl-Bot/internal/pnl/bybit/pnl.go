package bybitPnl

import (
	"log"

	"github.com/shopspring/decimal"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/bybit"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/types"
)

// ProcessTrade processes a trade or position to calculate realized or unrealized PnL
func ProcessTrade(record bybitTypes.ClosedPnLRecord, marketPrice decimal.Decimal, isOpen bool, pos ...bybitTypes.Position) {
	if !isOpen {
		// Process closed trade for realized PnL
		pnl, err := decimal.NewFromString(record.ClosedPnl)
		if err != nil {
			log.Printf("Invalid PnL for order %s: %v", record.OrderID, err)
			return
		}
		bybitCache.AddRealizedPnL(pnl)
		log.Printf("Processed closed trade: OrderID=%s, Symbol=%s, Side=%s, ClosedPnl=%s",
			record.OrderID, record.Symbol, record.Side, record.ClosedPnl)
	} else if len(pos) > 0 {
		// Process open position for unrealized PnL
		p := pos[0]
		entryPrice, err := decimal.NewFromString(p.EntryPrice)
		if err != nil {
	log.Printf("Invalid entry price for position %d: %v", p.PositionIdx, err)
			return
		}
		quantity, err := decimal.NewFromString(p.Size)
		if err != nil {
	log.Printf("Invalid size for position %d: %v", p.PositionIdx, err)
			return
		}

		var unrealizedPnL decimal.Decimal
		if p.Side == "Buy" {
			// Unrealized PnL = (Market Price - Entry Price) * Quantity
			unrealizedPnL = marketPrice.Sub(entryPrice).Mul(quantity)
		} else if p.Side == "Sell" {
			// Unrealized PnL = (Entry Price - Market Price) * Quantity
			unrealizedPnL = entryPrice.Sub(marketPrice).Mul(quantity)
		}

		bybitCache.AddUnrealizedPnL(unrealizedPnL)
		log.Printf("Processed open position: PositionIdx=%d, Symbol=%s, Side=%s, UnrealizedPnL=%s",
    p.PositionIdx, p.Symbol, p.Side, unrealizedPnL.String())
	}
}

// PrintRealizedPnL prints the current realized and unrealized PnL
func PrintRealizedPnL() {
	realizedPnL := bybitCache.GetRealizedPnL()
	unrealizedPnL := bybitCache.GetUnrealizedPnL()
	log.Printf("Current Realized PnL: %s USDT, Unrealized PnL: %s USDT", realizedPnL.String(), unrealizedPnL.String())
}