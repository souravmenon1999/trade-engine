package injectivePnl

import (
	"context"
	"strings"
	"sync"
	//"fmt"
	"regexp"
	"log"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/streams/injective"
	"github.com/shopspring/decimal"
explorer "github.com/InjectiveLabs/sdk-go/client/explorer"

	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
)



type GasCalculator struct {
	explorerClient explorer.ExplorerClient
	txHashes       map[string]bool
	mu             sync.Mutex
}

func NewGasCalculator(explorerClient explorer.ExplorerClient) *GasCalculator {
	return &GasCalculator{
		explorerClient: explorerClient,
		txHashes:       make(map[string]bool),
	}
}

func (c *GasCalculator) AddTxHash(txHash string) {
	if txHash == "" {
		return
	}
	normalized := strings.TrimPrefix(txHash, "0x")
	// Validate: must be 64 hex characters and not all zeros
	if len(normalized) != 64 || !regexp.MustCompile(`^[0-9a-fA-F]{64}$`).MatchString(normalized) || normalized == "0000000000000000000000000000000000000000000000000000000000000000" {
		log.Printf("Skipping invalid tx_hash: %s", txHash)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.txHashes[normalized] = true
}

func (c *GasCalculator) CalculateGas(ctx context.Context) (int64, error) {
	var totalGas int64
	for txHash := range c.txHashes {
		txDetails, err := c.explorerClient.GetTxByTxHash(ctx, txHash)
		if err != nil {
			log.Printf("Error fetching tx %s: %v", txHash, err)
			continue
		}
		totalGas += txDetails.Data.GasUsed
	}
	return totalGas, nil
}






func ProcessTrade(trade *derivativeExchangePB.DerivativeTrade, marketPrice decimal.Decimal) {
	marketID := trade.MarketId
	side := trade.PositionDelta.TradeDirection
	quantity, _ := decimal.NewFromString(trade.PositionDelta.ExecutionQuantity)
	
	// Convert prices from micro USDT to USDT
	priceMicro, _ := decimal.NewFromString(trade.PositionDelta.ExecutionPrice)
	price := priceMicro.Div(decimal.NewFromInt(1e6))
	
	feeMicro, _ := decimal.NewFromString(trade.Fee)
	fee := feeMicro.Div(decimal.NewFromInt(1e6))

	// Handle rebates
	if fee.LessThan(decimal.Zero) {
		rebate := fee.Neg()
		injectiveCache.AddFeeRebate(rebate)
		log.Printf("Rebate received: %s for trade %s (ExecutionSide: %s)", rebate, trade.TradeId, trade.ExecutionSide)
	} else if fee.GreaterThan(decimal.Zero) {
		log.Printf("Fee paid (no impact on PnL or rebates): %s for trade %s (ExecutionSide: %s)", fee, trade.TradeId, trade.ExecutionSide)
	}

	// Retrieve current position
	pos, exists := injectiveCache.GetPosition(marketID)
	if !exists {
		pos = injectiveCache.Position{Quantity: decimal.Zero, AverageEntryPrice: decimal.Zero}
	}

	log.Printf("Processing trade: %s %s at %s USDT (Fee: %s USDT, ExecutionSide: %s)", 
		side, quantity, price, fee, trade.ExecutionSide)

	// Convert market price to USDT if needed
	if marketPrice.IsZero() {
		marketPrice = injectiveStreams.GetLatestMarketPrice().Div(decimal.NewFromInt(1e6))
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
				pnlDelta := pos.AverageEntryPrice.Sub(price).Mul(closingQuantity)
				injectiveCache.AddRealizedPnL(pnlDelta)
				pos.Quantity = pos.Quantity.Add(closingQuantity)
			} else {
				closingQuantity := pos.Quantity.Neg()
				pnlDelta := pos.AverageEntryPrice.Sub(price).Mul(closingQuantity)
				injectiveCache.AddRealizedPnL(pnlDelta)
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
				pnlDelta := price.Sub(pos.AverageEntryPrice).Mul(closingQuantity)
				injectiveCache.AddRealizedPnL(pnlDelta)
				pos.Quantity = pos.Quantity.Sub(closingQuantity)
			} else {
				closingQuantity := pos.Quantity
				pnlDelta := price.Sub(pos.AverageEntryPrice).Mul(closingQuantity)
				injectiveCache.AddRealizedPnL(pnlDelta)
				remainingQuantity := quantity.Sub(closingQuantity)
				pos.Quantity = remainingQuantity.Neg()
				pos.AverageEntryPrice = price
			}
		}
	}

	// Calculate unrealized PnL
	unrealized := decimal.Zero
	if !pos.Quantity.IsZero() && !marketPrice.IsZero() {
		if pos.Quantity.GreaterThan(decimal.Zero) {
			unrealized = marketPrice.Sub(pos.AverageEntryPrice).Mul(pos.Quantity)
		} else {
			unrealized = pos.AverageEntryPrice.Sub(marketPrice).Mul(pos.Quantity.Neg())
		}
	}
	injectiveCache.AddUnrealizedPnL(unrealized)

	// Update or remove position
	if pos.Quantity.IsZero() {
		injectiveCache.DeletePosition(marketID)
	} else {
		injectiveCache.SetPosition(marketID, pos)
	}

	log.Printf("Updated position for %s: Quantity=%s, AvgEntry=%s USDT, RealizedPnL=%s USDT, UnrealizedPnL=%s USDT, FeeRebates=%s USDT, MarketPrice=%s USDT",
		marketID, pos.Quantity, pos.AverageEntryPrice, injectiveCache.GetRealizedPnL(), injectiveCache.GetUnrealizedPnL(), injectiveCache.GetFeeRebates(), marketPrice)
}



// var denomToSymbol = map[string]string{
//     "peggy0xdAC17F958D2ee523a2206206994597C13D831ec7": "USDT",
//     "inj": "INJ",
//     // Add more mappings as needed
// }

// Function to convert denomination to symbol
// func getSymbol(denom string) string {
//     if symbol, ok := denomToSymbol[denom]; ok {
//         return symbol
//     }
//     return denom // Return original denom if no mapping exists
// }

func PrintRealizedPnL() {
	totalUSD := injectiveCache.GetTotalUSD()
	if totalUSD == "" {
		totalUSD = "0" // Fallback if not set
	}
	log.Printf("Current Realized PnL: %s USDT, Unrealized PnL: %s USDT, Fee Rebates: %s USDT, Total Gas: %d, Total USD Balance: %s",
		injectiveCache.GetRealizedPnL(), injectiveCache.GetUnrealizedPnL(), injectiveCache.GetFeeRebates(), injectiveCache.GetTotalGas(), totalUSD)
}