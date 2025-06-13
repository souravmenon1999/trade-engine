package pnl

import (
	"log"

	"internal/cache"

	"github.com/shopspring/decimal"
	spotExchangePB "github.com/InjectiveLabs/sdk-go/exchange/spot_exchange_rpc/pb"
)

// ProcessTrade updates positions and calculates realized PnL for a trade
func ProcessTrade(trade *spotExchangePB.SpotTrade) {
	marketID := trade.MarketId
	side := trade.TradeDirection
	quantity, _ := decimal.NewFromString(trade.Price.Quantity)
	price, _ := decimal.NewFromString(trade.Price.Price)
	fee, _ := decimal.NewFromString(trade.Fee)

	// Get current position
	pos, exists := cache.GetPosition(marketID)
	if !exists {
		pos = cache.Position{Quantity: decimal.Zero, AverageEntryPrice: decimal.Zero}
	}

	log.Printf("Processing trade: %s %s at %s (Fee: %s)", side, quantity, price, fee)

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
				// Reduce short position
				closingQuantity := quantity
				pnlDelta := pos.AverageEntryPrice.Sub(price).Mul(closingQuantity).Sub(fee)
				cache.AddRealizedPnL(pnlDelta)
				pos.Quantity = pos.Quantity.Add(closingQuantity) // Less negative
			} else {
				// Close short and open long
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
				// Reduce long position
				closingQuantity := quantity
				pnlDelta := price.Sub(pos.AverageEntryPrice).Mul(closingQuantity).Sub(fee)
				cache.AddRealizedPnL(pnlDelta)
				pos.Quantity = pos.Quantity.Sub(closingQuantity)
			} else {
				// Close long and open short
				closingQuantity := pos.Quantity
				pnlDelta := price.Sub(pos.AverageEntryPrice).Mul(closingQuantity).Sub(fee.Mul(closingQuantity.Div(quantity)))
				cache.AddRealizedPnL(pnlDelta)
				remainingQuantity := quantity.Sub(closingQuantity)
				pos.Quantity = remainingQuantity.Neg()
				pos.AverageEntryPrice = price
			}
		}
	}

	// Update or delete position
	if pos.Quantity.IsZero() {
		cache.DeletePosition(marketID)
	} else {
		cache.SetPosition(marketID, pos)
	}

	log.Printf("Updated position for %s: Quantity=%s, AvgEntry=%s, RealizedPnL=%s", 
		marketID, pos.Quantity, pos.AverageEntryPrice, cache.GetRealizedPnL())
}

// PrintRealizedPnL logs the current realized PnL
func PrintRealizedPnL() {
	log.Printf("Current Realized PnL: %s", cache.GetRealizedPnL())
}