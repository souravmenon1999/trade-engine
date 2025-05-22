package injective

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/InjectiveLabs/sdk-go/client/chain"
	"github.com/InjectiveLabs/sdk-go/client/exchange"
	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	"github.com/souravmenon1999/trade-engine/framed/exchange/injective/grpc"
)

// InjectiveClient holds the trade and updates clients
type InjectiveClient struct {
	tradeClient   *chain.ChainClient
	updatesClient *exchange.ExchangeClient
	senderAddress string
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewInjectiveClient initializes the InjectiveClient with trade and updates clients
func NewInjectiveClient(networkName, lb, privKey string, orderUpdateCallback func([]byte)) (*InjectiveClient, error) {
	grpcClient, err := grpc.NewInjectiveGRPCClient(networkName, lb)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	tradeClient, err := grpcClient.GetTradeClient(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get trade client: %w", err)
	}

	updatesClient, err := grpcClient.GetUpdatesClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get updates client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &InjectiveClient{
		tradeClient:   tradeClient,
		updatesClient: updatesClient,
		senderAddress: tradeClient.FromAddress().String(),
		ctx:           ctx,
		cancel:        cancel,
	}

	// Start subscription automatically if callback is provided
	if orderUpdateCallback != nil {
		go client.SubscribeOrderHistory(orderUpdateCallback)
	}

	return client, nil
}

// SubscribeOrderHistory subscribes to order history updates
func (c *InjectiveClient) SubscribeOrderHistory(callback func([]byte)) {
	req := &derivativeExchangePB.StreamOrdersHistoryRequest{}
	stream, err := (*c.updatesClient).StreamOrdersHistory(c.ctx, req)
	if err != nil {
		log.Printf("Failed to stream order history: %v", err)
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			res, err := stream.Recv()
			if err != nil {
				log.Printf("Error receiving order history: %v", err)
				return
			}
			data, err := json.Marshal(res)
			if err != nil {
				log.Printf("Error marshaling response: %v", err)
				continue
			}
			callback(data)
		}
	}
}

// PlaceOrder executes a trade using the chain client
func (c *InjectiveClient) PlaceOrder(order *chain.Order) error {
	log.Printf("Placing order for sender %s", c.senderAddress)
	// Placeholder: Add actual order placement logic here
	return nil
}