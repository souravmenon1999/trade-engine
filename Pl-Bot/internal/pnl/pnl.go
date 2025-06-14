package pnl

import (
	"log"

	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/streams"

	"github.com/shopspring/decimal"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
)

// ProcessTrade processes a trade, updating position and PnL.
func ProcessTrade(trade *derivativeExchangePB.DerivativeTrade, marketPrice decimal.Decimal) {
	marketID := trade.MarketId
	side := trade.PositionDelta.TradeDirection
	quantity, _ := decimal.NewFromString(trade.PositionDelta.ExecutionQuantity)
	price, _ := decimal.NewFromString(trade.PositionDelta.ExecutionPrice)
	fee, _ := decimal.NewFromString(trade.Fee)

	// Retrieve current position
	pos, exists := cache.GetPosition(marketID)
	if !exists {
		pos = cache.Position{Quantity: decimal.Zero, AverageEntryPrice: decimal.Zero}
	}

	log.Printf("Processing trade: %s %s at %s (Fee: %s)", side, quantity, price, fee)

	// Use provided market price or fetch latest if zero
	if marketPrice.IsZero() {
		marketPrice = streams.GetLatestMarketPrice()
		if marketPrice.IsZero() {
			log.Printf("No market price available for %s, unrealized PnL set to zero", marketID)
		}
	}

	if side == "buy" {
		if pos.Quantity.GreaterThanOrEqual(decimal.Zero) {
			// Increase long position
			newQuantity := pos.Quantity.Add(quantity)
			if newQuantity.GreaterThan(decimal.Zero) {
				pos.AverageEntryPrice = pos.Quantity.Mul(pos.AverageEntryPrice).Add(quantity.Mul(price)).Div(newQuantity)
			}
			pos.Quantity = newQuantity
		} else {
			// Reduce short position or flip to long
			if quantity.LessThanOrEqual(pos.Quantity.Neg()) {
				closingQuantity := quantity
				pnlDelta := pos.AverageEntryPrice.Sub(price).Mul(closingQuantity).Sub(fee)
				cache.AddRealizedPnL(pnlDelta)
				pos.Quantity = pos.Quantity.Add(closingQuantity)
			} else {
				closingQuantity := pos.Quantity.Neg()
				pnlDelta := pos.AverageEntryPrice.Sub(price).Mul(closingQuantity).Sub(fee.Mul(closingQuantity.Div(quantity)))
				cache.AddRealizedPnL(pnlDelta)
				remainingQuantity := quantity.Sub(closingQuantity)
				pos.Quantity = remainingQuantity
				pos.AverageEntryPrice = price
			}
		}
	} else if side == "sell" {
		if pos.Quantity.LessThanOrEqual(decimal.Zero) {
			// Increase short position
			newQuantity := pos.Quantity.Sub(quantity)
			if newQuantity.LessThan(decimal.Zero) {
				pos.AverageEntryPrice = pos.Quantity.Neg().Mul(pos.AverageEntryPrice).Add(quantity.Mul(price)).Div(newQuantity.Neg())
			}
			pos.Quantity = newQuantity
		} else {
			// Reduce long position or flip to short
			if quantity.LessThanOrEqual(pos.Quantity) {
				closingQuantity := quantity
				pnlDelta := price.Sub(pos.AverageEntryPrice).Mul(closingQuantity).Sub(fee)
				cache.AddRealizedPnL(pnlDelta)
				log.Printf("Sell (Reduce Long): pnlDelta=%s, realizedPnL_before=%s", pnlDelta, cache.GetRealizedPnL())
				pos.Quantity = pos.Quantity.Sub(closingQuantity)
			} else {
				closingQuantity := pos.Quantity
				log.Printf("Sell (Flip to Short): marketID=%s, price=%s, pos.AverageEntryPrice=%s, closingQuantity=%s, fee=%s, quantity=%s",
					marketID, price, pos.AverageEntryPrice, closingQuantity, fee, quantity)
				pnlDelta := price.Sub(pos.AverageEntryPrice).Mul(closingQuantity).Sub(fee.Mul(closingQuantity.Div(quantity)))
				log.Printf("Sell (Flip to Short): pnlDelta=%s, realizedPnL_before=%s", pnlDelta, cache.GetRealizedPnL())
				cache.AddRealizedPnL(pnlDelta)
				log.Printf("Sell (Flip to Short): realizedPnL_after=%s", cache.GetRealizedPnL())
				remainingQuantity := quantity.Sub(closingQuantity)
				log.Printf("Sell (Flip to Short): remainingQuantity=%s, new_pos.Quantity=%s, new_pos.AverageEntryPrice=%s",
					remainingQuantity, remainingQuantity.Neg(), price)
				pos.Quantity = remainingQuantity.Neg()
				pos.AverageEntryPrice = price
			}
		}
	}

	// Calculate unrealized PnL
	unrealizedPnL := decimal.Zero
	if !pos.Quantity.IsZero() && !marketPrice.IsZero() {
		if pos.Quantity.GreaterThan(decimal.Zero) {
			// Long position: (Market Price - Avg Entry Price) * Quantity
			unrealizedPnL = marketPrice.Sub(pos.AverageEntryPrice).Mul(pos.Quantity)
		} else {
			// Short position: (Avg Entry Price - Market Price) * |Quantity|
			unrealizedPnL = pos.AverageEntryPrice.Sub(marketPrice).Mul(pos.Quantity.Neg())
		}
	}
	cache.AddUnrealizedPnL(unrealizedPnL)

	// Update or remove position
	if pos.Quantity.IsZero() {
		cache.DeletePosition(marketID)
	} else {
		cache.SetPosition(marketID, pos)
	}

	log.Printf("Updated position for %s: Quantity=%s, AvgEntry=%s, RealizedPnL=%s, UnrealizedPnL=%s, MarketPrice=%s",
		marketID, pos.Quantity, pos.AverageEntryPrice, cache.GetRealizedPnL(), cache.GetUnrealizedPnL(), marketPrice)
}

// PrintRealizedPnL prints the current realized and unrealized PnL.
func PrintRealizedPnL() {
	log.Printf("Current Realized PnL: %s, Unrealized PnL: %s", cache.GetRealizedPnL(), cache.GetUnrealizedPnL())
}