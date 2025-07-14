package injectivePnl

import (
	"context"
	//"strings"
	"sync"
	"strings"
	//"fmt"
	//"regexp"
	"math/big"
	//"strconv"
	"log"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/streams/injective"
	"github.com/shopspring/decimal"
	//explorer "github.com/InjectiveLabs/sdk-go/client/explorer"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
)


var (
    positions = make(map[string]*Position)
    mu        sync.RWMutex
    totalGas  uint64
)

type Position struct {
    Quantity      *big.Float
    AvgEntryPrice *big.Float
    RealizedPnL   *big.Float
    UnrealizedPnL *big.Float
    FeeRebates    *big.Float
    MarketPrice   *big.Float
}

type GasCalculator struct {
    txHashes map[string]struct{}
	
}

func NewGasCalculator() *GasCalculator {
    return &GasCalculator{
        txHashes: make(map[string]struct{}),
    }
}

func (c *GasCalculator) AddTxHash(txHash string) {
    c.txHashes[txHash] = struct{}{}
}

func (c *GasCalculator) CalculateGasWithChainClient(ctx context.Context, chainClient chainclient.ChainClient) (decimal.Decimal, error) {
    var totalGasFee decimal.Decimal
    for txHash := range c.txHashes {
        resp, err := chainClient.GetTx(ctx, txHash)
        if err != nil {
            log.Printf("Error fetching tx %s: %v", txHash, err)
            continue
        }
        for _, event := range resp.TxResponse.Events {
            if event.Type == "tx" {
                for _, attr := range event.Attributes {
                    if attr.Key == "fee" && strings.HasSuffix(attr.Value, "inj") {
                        feeAmountStr := strings.TrimSuffix(attr.Value, "inj")
                        feeAmount, err := decimal.NewFromString(feeAmountStr)
                        if err != nil {
                            log.Printf("Error parsing fee amount for tx %s: %v", txHash, err)
                            continue
                        }
                        totalGasFee = totalGasFee.Add(feeAmount.Div(decimal.NewFromInt(1e18)))
                    }
                }
            }
        }
    }
	log.Printf("gas %v", totalGasFee )
    return totalGasFee, nil
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
	log.Printf("Current Realized PnL: %s USDT, Unrealized PnL: %s USDT, Fee Rebates: %s USDT, Total Gas: %v, Total USD Balance: %s",
		injectiveCache.GetRealizedPnL(), injectiveCache.GetUnrealizedPnL(), injectiveCache.GetFeeRebates(), injectiveCache.GetTotalGas(), totalUSD)
}

func GetPnLStats() (float64, float64, float64, uint64) {
    mu.RLock()
    defer mu.RUnlock()
    var realizedPnL, unrealizedPnL, feeRebates float64
    for _, pos := range positions {
        if pos.Quantity.Cmp(big.NewFloat(0)) != 0 {
            pos.UnrealizedPnL = new(big.Float).Mul(pos.Quantity, new(big.Float).Sub(pos.MarketPrice, pos.AvgEntryPrice))
        }
        r, _ := pos.RealizedPnL.Float64()
        u, _ := pos.UnrealizedPnL.Float64()
        f, _ := pos.FeeRebates.Float64()
        realizedPnL += r
        unrealizedPnL += u
        feeRebates += f
    }
    return realizedPnL, unrealizedPnL, feeRebates, totalGas
}