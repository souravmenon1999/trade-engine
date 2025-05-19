package injective

import (
	"context"
	"log"

	"github.com/InjectiveLabs/sdk-go/client/exchange"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
)

// InjectiveUpdatesClient handles streaming updates from Injective using gRPC.
type InjectiveUpdatesClient struct {
	exchangeClient exchange.ExchangeClient
}

// NewInjectiveUpdatesClient initializes an updates client for streaming.
func NewInjectiveUpdatesClient(exchangeClient exchange.ExchangeClient) *InjectiveUpdatesClient {
	return &InjectiveUpdatesClient{
		exchangeClient: exchangeClient,
	}
}

// StreamOrderHistory streams order history updates for a given market and subaccount.
func (c *InjectiveUpdatesClient) StreamOrderHistory(ctx context.Context, marketId, subaccountId string, handler func(*derivativeExchangePB.StreamOrdersHistoryResponse)) error {
	req := &derivativeExchangePB.StreamOrdersHistoryRequest{
		MarketId:     marketId,
		SubaccountId: subaccountId,
	}
	stream, err := c.exchangeClient.StreamHistoricalDerivativeOrders(ctx, req)
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				res, err := stream.Recv()
				if err != nil {
					log.Printf("Error receiving stream data: %v", err)
					return
				}
				handler(res)
			}
		}
	}()
	return nil
}