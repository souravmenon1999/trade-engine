package injectiveStreams

import (
	"context"
	"log"
	"strings"
	"sync"
    "encoding/json"
	"github.com/InjectiveLabs/sdk-go/client/common"
	exchangeclient "github.com/InjectiveLabs/sdk-go/client/exchange"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	"github.com/shopspring/decimal"
)

// DerivativeMarketResponse mirrors the SDK's response structure for GetDerivativeMarket.
type PortfolioResponse struct {
    Type         string `json:"type"`
    Denom        string `json:"denom"`
    Amount       string `json:"amount"`
    SubaccountId string `json:"subaccount_id"`
}
type DerivativeMarketResponse struct {
    Market struct {
        MarketID              string `json:"market_id"`
        MarketStatus          string `json:"market_status"`
        Ticker                string `json:"ticker"`
        OracleBase            string `json:"oracle_base"`
        OracleQuote           string `json:"oracle_quote"`
        OracleType            string `json:"oracle_type"`
        OracleScaleFactor     int32  `json:"oracle_scale_factor"`
        InitialMarginRatio    string `json:"initial_margin_ratio"`
        MaintenanceMarginRatio string `json:"maintenance_margin_ratio"`
        QuoteDenom            string `json:"quote_denom"`
        MakerFeeRate          string `json:"maker_fee_rate"`
        TakerFeeRate          string `json:"taker_fee_rate"`
        ServiceProviderFee    string `json:"service_provider_fee"`
        IsPerpetual           bool   `json:"is_perpetual"`
        MinPriceTickSize      string `json:"-night_tick_size"`
        MinQuantityTickSize   string `json:"min_quantity_tick_size"`
        MinNotional           string `json:"min_notional"`
    } `json:"market"`
}

var (
	latestMarketPrice decimal.Decimal
	priceMutex        sync.RWMutex
)

func FetchMarketDetails(client exchangeclient.ExchangeClient,ctx context.Context, marketID string) (*DerivativeMarketResponse, error) {
    network := common.LoadNetwork("mainnet", "lb")
    exchangeClient, err := exchangeclient.NewExchangeClient(network)
    if err != nil {
        return nil, err
    }

    res, err := exchangeClient.GetDerivativeMarket(ctx, marketID)
    if err != nil {
        return nil, err
    }

	log.Printf("reponse: %v", res)

    response := &DerivativeMarketResponse{}
    response.Market.MarketID = res.Market.MarketId
    response.Market.MarketStatus = res.Market.MarketStatus
    response.Market.Ticker = res.Market.Ticker
    response.Market.OracleBase = res.Market.OracleBase
    response.Market.OracleQuote = res.Market.OracleQuote
    response.Market.OracleType = res.Market.OracleType
    response.Market.OracleScaleFactor = int32(res.Market.OracleScaleFactor)
    response.Market.InitialMarginRatio = res.Market.InitialMarginRatio
    response.Market.MaintenanceMarginRatio = res.Market.MaintenanceMarginRatio
    response.Market.QuoteDenom = res.Market.QuoteDenom
    response.Market.MakerFeeRate = res.Market.MakerFeeRate
    response.Market.TakerFeeRate = res.Market.TakerFeeRate
    response.Market.ServiceProviderFee = res.Market.ServiceProviderFee
    response.Market.IsPerpetual = res.Market.IsPerpetual
    response.Market.MinPriceTickSize = res.Market.MinPriceTickSize
    response.Market.MinQuantityTickSize = res.Market.MinQuantityTickSize
    response.Market.MinNotional = res.Market.MinNotional

    return response, nil
}

// SubscribeToTradeStream subscribes to live trade updates and returns a receiver function.
func SubscribeToTradeStream(client exchangeclient.ExchangeClient,subaccountID, marketID string) (func() (*derivativeExchangePB.DerivativeTrade, error), error) {
	network := common.LoadNetwork("mainnet", "lb")
	exchangeClient, err := exchangeclient.NewExchangeClient(network)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	req := derivativeExchangePB.StreamTradesV2Request{
		SubaccountId: subaccountID,
		MarketId:     marketID,
	}

	stream, err := exchangeClient.StreamDerivativeV2Trades(ctx, &req)
	if err != nil {
		return nil, err
	}

	return func() (*derivativeExchangePB.DerivativeTrade, error) {
		res, err := stream.Recv()
		if err != nil {
			log.Printf("Trade stream error: %v", err)
			return nil, err
		}
		return res.Trade, nil
	}, nil
}

// SubscribeToMarketPriceStream subscribes to price updates using oracle details.
func SubscribeToMarketPriceStream(client exchangeclient.ExchangeClient, baseSymbol, quoteSymbol, oracleType string) {
	network := common.LoadNetwork("mainnet", "lb")
	exchangeClient, err := exchangeclient.NewExchangeClient(network)
	if err != nil {
		log.Fatalf("Failed to create exchange client for price stream: %v", err)
	}

	ctx := context.Background()
	stream, err := exchangeClient.StreamPrices(ctx, baseSymbol, quoteSymbol, strings.ToLower(oracleType))
	if err != nil {
		log.Fatalf("Failed to subscribe to price stream: %v", err)
	}


    
	for {
		res, err := stream.Recv()
		if err != nil {
			log.Printf("Price stream error: %v, attempting to reconnect...", err)
			continue
		}
		price, err := decimal.NewFromString(res.Price)
		if err != nil {
			log.Printf("Failed to parse price: %v", err)
			continue
		}
		priceMutex.Lock()
		latestMarketPrice = price
		priceMutex.Unlock()
		//log.Printf("Updated market price for %s/%s: %s", baseSymbol, quoteSymbol, price)
	}
}

// GetLatestMarketPrice returns the latest market price from the stream.
func GetLatestMarketPrice() decimal.Decimal {
	priceMutex.RLock()
	defer priceMutex.RUnlock()
	return latestMarketPrice
}

type StreamCallback func(res *PortfolioResponse, err error)


func SubscribeToAccountPortfolioStream(
    client exchangeclient.ExchangeClient,
    accountAddress, subaccountID, portfolioType string,
    callback StreamCallback,
) error {
    log.Println("Starting account portfolio stream subscription")
    ctx := context.Background()
    stream, err := client.StreamAccountPortfolio(ctx, accountAddress, subaccountID, portfolioType)
    if err != nil {
        return err
    }

    go func() {
        for {
            res, err := stream.Recv()
            if err != nil {
                log.Printf("Account portfolio stream error: %v", err)
                callback(nil, err)
                
                // Attempt to reconnect after delay
                //time.Sleep(3 * time.Second)
                newStream, err := client.StreamAccountPortfolio(ctx, accountAddress, subaccountID, portfolioType)
                if err != nil {
                    log.Printf("Reconnection failed: %v", err)
                    continue
                }
                stream = newStream
                continue
            }
            
            // Process response
            jsonBytes, _ := json.Marshal(res)
            var portfolio PortfolioResponse
            if err := json.Unmarshal(jsonBytes, &portfolio); err == nil {
                callback(&portfolio, nil)
            } else {
                log.Printf("Error unmarshaling portfolio response: %v", err)
            }
        }
    }()
    
    return nil
}