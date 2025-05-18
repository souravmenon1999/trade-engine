// internal/exchange/injective/client.go - Functional Injective Client (Corrected)
package injective

import (
	"context"
	"fmt"
	"time"
	"math/big" // Required for decimal.NewFromBigInt
    chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
    chain  "github.com/InjectiveLabs/sdk-go/client"
	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	projecttypes "github.com/souravmenon1999/trade-engine/internal/types" // Use projecttypes alias for clarity
	"log/slog"
	"github.com/google/uuid" // For generating Client Order IDs (Cids)
	"github.com/InjectiveLabs/sdk-go/client/common" // For common client setup, LoadNetwork
	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types" // For MsgBatchUpdateOrders, OrderType enum
	"github.com/shopspring/decimal"
	// Cosmos SDK imports

	cosmossdkmath "cosmossdk.io/math"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
)

// Client implements the ExchangeClient interface for Injective.
type Client struct {
    cfg    *config.InjectiveConfig
    logger *slog.Logger

    // --- Functional Injective SDK components ---
    chainClient    chainclient.ChainClient // Single instance for signing/broadcasting queue
    accountAddress string                  // Bech32 wallet address string
    subaccountID   string                  // Default subaccount ID (Hex, from config or empty)

    // Context for the client's operations
    ctx    context.Context
    cancel context.CancelFunc

    // Injective client does NOT provide a live orderbook feed in this setup.
    orderbook *projecttypes.Orderbook // Always nil
}

// NewClient creates a new Functional Injective client.
func NewClient(ctx context.Context, cfg *config.InjectiveConfig) (*Client, error) {
    clientCtx, cancel := context.WithCancel(ctx)

    client := &Client{
        cfg:    cfg,
        logger: logging.GetLogger().With("exchange", "injective"),
        ctx:    clientCtx,
        cancel: cancel,
        orderbook: nil, // Injective client doesn't maintain a live OB in this setup
        subaccountID: "", // Will be set below
    }

    // 1. Load Network configuration (e.g., testnet, mainnet)
    network := common.LoadNetwork(cfg.ChainConfig, "lb")

    // 2. Create CometBFT RPC Client
    tmClient, err := rpchttp.New(network.TmEndpoint, "/websocket")
    if err != nil {
        client.logger.Error("Failed to create CometBFT RPC client", "error", err)
        return nil, projecttypes.TradingError{
            Code:    projecttypes.ErrConnectionFailed,
            Message: "Failed to connect to Injective RPC endpoint",
            Wrapped: err,
        }
    }

    // 3. Initialize keyring and sender address
    senderAddress, cosmosKeyring, err := chainclient.InitCosmosKeyring(
        "",             // No directory for memory backend
        "injective",    // App name
        "memory",       // Use in-memory backend
        "default",      // User name (arbitrary)
        "",             // No password for memory backend
        cfg.PrivateKey, // Private key from config
        false,          // Indicate private key usage (not mnemonic)
    )
    if err != nil {
        client.logger.Error("Failed to initialize keyring", "error", err)
        return nil, projecttypes.TradingError{
            Code:    projecttypes.ErrConnectionFailed,
            Message: "Failed to initialize keyring",
            Wrapped: err,
        }
    }
    client.accountAddress = senderAddress.String()

    // 4. Create Cosmos SDK Client Context
    sdkClientCtx, err := chainclient.NewClientContext(
        network.ChainId,
        client.accountAddress,
        cosmosKeyring,
    )
    if err != nil {
        client.logger.Error("Failed to create Cosmos SDK client context", "error", err)
        return nil, projecttypes.TradingError{
            Code:    projecttypes.ErrConnectionFailed,
            Message: "Failed to setup Injective client context",
            Wrapped: err,
        }
    }
    sdkClientCtx = sdkClientCtx.WithNodeURI(network.TmEndpoint).WithClient(tmClient)

    // 5. Create Injective Chain Client
    chainClient, err := chainclient.NewChainClient(
        sdkClientCtx,
        network,
        common.OptionGasPrices(chain.DefaultGasPriceWithDenom),
    )
    if err != nil {
        client.logger.Error("Failed to create Injective chain client", "error", err)
        return nil, projecttypes.TradingError{
            Code:    projecttypes.ErrConnectionFailed,
            Message: "Failed to initialize Injective chain client",
            Wrapped: err,
        }
    }
    client.chainClient = chainClient

    // Set default subaccount ID using SDK method
    client.subaccountID = chainClient.DefaultSubaccount(senderAddress).String()
    client.logger.Info("Injective wallet initialized",
        "address", client.accountAddress,
        "subaccountID", client.subaccountID)

    client.logger.Info("Injective client initialization complete.")

    return client, nil
}


// GetExchangeType returns the type of this exchange client.
func (c *Client) GetExchangeType() projecttypes.ExchangeType {
	return projecttypes.ExchangeInjective
}

// GetOrderbook returns nil for the Injective client in this setup.
func (c *Client) GetOrderbook() *projecttypes.Orderbook {
	return c.orderbook // This will be nil
}

// SubscribeOrderbook is not implemented for the Injective trading client.
func (c *Client) SubscribeOrderbook(ctx context.Context, symbol string) error {
	c.logger.Warn("SubscribeOrderbook is not implemented for the Injective client.")
	return nil
}

// translateOrderDataToSpotLimitOrder converts a projecttypes.Order to an Injective spottypes.SpotOrder.

func (c *Client) translateOrderDataToSpotLimitOrder(
	order projecttypes.Order,
	senderAddress string, // Bech32 address string
	marketID string,
	// In a real system, you'd use c.marketsAssistant here for precise scaling
) (*exchangetypes.SpotOrder, error) {
	if order.Exchange != projecttypes.ExchangeInjective {
		return nil, fmt.Errorf("order is not for Injective")
	}
	if order.Instrument == nil {
		return nil, fmt.Errorf("order is missing instrument details")
	}
	// Add checks here to ensure the order's instrument matches the marketID type (Spot/Derivative)
	// and the marketID itself is correct based on Instrument.Symbol

	// --- Conversion Logic (Simplified Placeholder - NEEDS REPLACING) ---
	

	priceDecimal := decimal.NewFromBigInt(new(big.Int).SetUint64(order.Price), 0)
	quantityDecimal := decimal.NewFromBigInt(new(big.Int).SetUint64(order.Quantity), 0)

	priceLegacyDec, err := cosmossdkmath.LegacyNewDecFromStr(priceDecimal.String())
	if err != nil {
		return nil, fmt.Errorf("failed to convert price decimal to LegacyDec: %w", err)
	}
	quantityLegacyDec, err := cosmossdkmath.LegacyNewDecFromStr(quantityDecimal.String())
	if err != nil {
		return nil, fmt.Errorf("failed to convert quantity decimal to LegacyDec: %w", err)
	}

	// --- End Simplified Scaling Placeholder ---


	// Determine Injective OrderType
	injOrderType := exchangetypes.OrderType_BUY // Default
	switch order.Side {
	case projecttypes.Buy:
		injOrderType = exchangetypes.OrderType_BUY
	case projecttypes.Sell:
		injOrderType = exchangetypes.OrderType_SELL
	default:
		return nil, fmt.Errorf("unsupported order side for Injective: %s", order.Side)
	}

	// Client Order ID (Cid) should be unique per order within a batch and subaccount
	clientOrderID := uuid.New().String() // Use UUID for uniqueness

	// Create the Injective SDK SpotOrder struct
	spotOrder := &exchangetypes.SpotOrder{
		MarketId: marketID,
		OrderInfo: exchangetypes.OrderInfo{ // Use exchangetypes.OrderInfo
			SubaccountId: c.subaccountID, // Use client's subaccount ID
			FeeRecipient: senderAddress, // Fee recipient is often the sender
			Price:        priceLegacyDec,    // Use the legacy decimal price
			Quantity:     quantityLegacyDec, // Use the legacy decimal quantity
			Cid:          clientOrderID,     // Use the generated Client ID
		},
		OrderType: injOrderType,
	}

	// Note: For Derivative orders, you'd use derivative_types.DerivativeOrder
	// and provide Leverage, IsReduceOnly, etc.

	return spotOrder, nil // Return the SDK SpotOrder struct
}


// SubmitOrder sends a single order to the Injective chain.
// Implemented using MsgBatchUpdateOrders with one order.
func (c *Client) SubmitOrder(ctx context.Context, order projecttypes.Order) (string, error) {
	if order.Exchange != projecttypes.ExchangeInjective {
		return "", projecttypes.TradingError{
			Code:    projecttypes.ErrOrderSubmissionFailed,
			Message: "Order is not intended for Injective",
			Wrapped: fmt.Errorf("order target exchange: %s", order.Exchange),
		}
	}

	// Use the configured MarketID for the order
	marketID := c.cfg.MarketID


	c.logger.Info("Submitting single order to Injective via BatchUpdate...", "order", order, "market_id", marketID)

	// --- Translate Order to Injective SDK Message ---
	spotOrderToCreate, err := c.translateOrderDataToSpotLimitOrder(
		order,
		c.accountAddress, // Pass Bech32 address string
		marketID, // Pass market ID
		// Pass marketsAssistant here
	)
	if err != nil {
		c.logger.Error("Failed to translate single order to Injective message", "error", err)
		return "", projecttypes.TradingError{
			Code: projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to translate order message",
			Wrapped: err,
		}
	}

	// --- Create MsgBatchUpdateOrders ---
	// For a single order, create a batch message with only one order to create.
	msg := createBatchUpdateMessage(
		c.accountAddress, // Sender address string
		c.subaccountID, // Subaccount ID is required if ordersToCreate is not empty
		nil, // No spot markets to cancel all
		nil, // No derivative markets to cancel all
		[]*exchangetypes.SpotOrder{spotOrderToCreate}, // Use exchangetypes.SpotOrder
		nil, // No derivative orders to create
	)

	// --- Broadcast Transaction (Fire-and-Forget) ---
	// Use the stored chainClient's QueueBroadcastMsg for asynchronous, sequential broadcasting.
	err = c.chainClient.QueueBroadcastMsg(msg)

	if err != nil {
		c.logger.Error("Failed to queue single order broadcast", "error", err, "order", order, "market_id", marketID)
		return "", projecttypes.TradingError{
			Code: projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to queue transaction for single order",
			Wrapped: err,
		}
	}

	c.logger.Info("Single order queued for broadcast", "order", order, "client_order_id", spotOrderToCreate.OrderInfo.Cid, "market_id", marketID)

	// QueueBroadcastMsg returns immediately. We don't have the Tx hash yet.
	
	return spotOrderToCreate.OrderInfo.Cid, nil // Access Cid from OrderInfo
}

// CancelAllOrders cancels all open orders for the configured market and sender's subaccount.
// It uses MsgBatchUpdateOrders to cancel all orders for the specific market ID.
func (c *Client) CancelAllOrders(ctx context.Context, instrument *projecttypes.Instrument) error {
	if instrument == nil {
		return fmt.Errorf("instrument is nil")
	}
	// Use the configured MarketID as the target for cancellation.
	marketID := c.cfg.MarketID

	// Optional: Log a warning if the instrument symbol doesn't match the configured market intent
	// if instrument.Symbol != ??? // Need a way to map cfg.MarketID back to a symbol


	c.logger.Info("Attempting to cancel all orders for market via BatchUpdate...", "market_id", marketID, "subaccount_id", c.subaccountID)

	// --- Create MsgBatchUpdateOrders for Cancellation ---
	// Create a batch message with only the market ID to cancel all orders for.
	msg := createBatchUpdateMessage(
		c.accountAddress, // Sender address string
		c.subaccountID,
		[]string{marketID}, // Spot market IDs to cancel all
		nil, // Derivative market IDs to cancel all (if applicable)
		nil, // No spot orders to create
		nil, // No derivative orders to create
	)

	// --- Broadcast Transaction (Fire-and-Forget) ---
	// Use the stored chainClient's QueueBroadcastMsg.
	err := c.chainClient.QueueBroadcastMsg(msg)

	if err != nil {
		c.logger.Error("Failed to queue cancel all orders broadcast", "error", err, "market_id", marketID)
		return projecttypes.TradingError{
			Code: projecttypes.ErrOrderSubmissionFailed, // Or a new code like ErrCancellationFailed
			Message: "Failed to queue transaction for cancelling all orders",
			Wrapped: err,
		}
	}

	c.logger.Info("Cancel all orders request queued for broadcast", "market_id", marketID)

	return nil // Success means it's queued, not necessarily executed yet
}

// ReplaceQuotes atomically cancels existing quotes for the configured market
func (c *Client) ReplaceQuotes(ctx context.Context, instrument *projecttypes.Instrument, ordersToPlace []*projecttypes.Order) ([]string, error) {
	if instrument == nil {
		return nil, fmt.Errorf("instrument is nil")
	}
	// Use the configured MarketID as the target for the batch update.
	marketID := c.cfg.MarketID


	if len(ordersToPlace) == 0 {
		c.logger.Info("ReplaceQuotes called with no orders to place. Will only cancel existing.", "market_id", marketID)
		// Allowing 0 orders makes this method act as a "CancelAll" if no new quotes are generated.
	}


	c.logger.Info("Attempting to replace quotes via BatchUpdate...",
		"market_id", marketID, "subaccount_id", c.subaccountID, "orders_to_place_count", len(ordersToPlace))

	// --- Translate Orders to Injective SDK Messages ---
	// Assuming Spot orders. Iterate through the provided orders.
	spotOrdersToCreate := make([]*exchangetypes.SpotOrder, 0, len(ordersToPlace)) // Use exchangetypes.SpotOrder
	newOrderCids := []string{} // Collect Cids for the new orders

	for _, order := range ordersToPlace {
		if order == nil {
			c.logger.Warn("Skipping nil order in ReplaceQuotes list")
			continue
		}
		if order.Exchange != projecttypes.ExchangeInjective {
			c.logger.Error("Skipping order not intended for Injective in ReplaceQuotes list", "order", order)
			continue
		}
		// In a real system, verify order.Instrument matches the target market type (Spot/Derivative)
		// and the marketID matches the instrument.

		// Assuming Spot market based on strategy
		spotOrder, err := c.translateOrderDataToSpotLimitOrder(
			*order, // Pass order by value
			c.accountAddress, // Pass Bech32 address string
			marketID, // Pass market ID
			// Pass marketsAssistant here
		)
		if err != nil {
			c.logger.Error("Failed to translate order for batch update", "error", err, "order", order)
			// Skip the single bad order and log.
			continue
		}
		spotOrdersToCreate = append(spotOrdersToCreate, spotOrder)
		newOrderCids = append(newOrderCids, spotOrder.OrderInfo.Cid) // Collect the Client IDs (strings)
	}

	if len(spotOrdersToCreate) == 0 && len(ordersToPlace) > 0 {
		// This means all attempted translations failed
		return nil, projecttypes.TradingError{
			Code: projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to translate any provided orders for batch update",
		}
	}

	// --- Create MsgBatchUpdateOrders ---
	// This message cancels ALL existing orders for the sender's subaccount
	// on the specified market ID and then attempts to create the new orders.
	msg := createBatchUpdateMessage(
		c.accountAddress, // Sender address string
		c.subaccountID, // Subaccount ID is REQUIRED for CancelAll
		[]string{marketID}, // Spot market IDs to cancel ALL
		nil, // Derivative market IDs to cancel ALL (if applicable)
		spotOrdersToCreate, // List of spot orders to create (exchangetypes.SpotOrder)
		nil, // List of derivative orders to create (if applicable)
	)

	// --- Broadcast Transaction (Fire-and-Forget) ---
	// Use the stored chainClient's QueueBroadcastMsg.
	err := c.chainClient.QueueBroadcastMsg(msg)

	if err != nil {
		c.logger.Error("Failed to queue batch update broadcast", "error", err, "market_id", marketID, "orders_count", len(spotOrdersToCreate))
		return nil, projecttypes.TradingError{
			Code: projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to queue transaction for batch order update",
			Wrapped: err,
		}
	}

	c.logger.Info("Batch update (cancel all + place new) request queued for broadcast",
		"market_id", marketID, "orders_queued_count", len(spotOrdersToCreate), "new_order_cids", newOrderCids)

	// Fire-and-Forget at the application level. Return the Cids of the orders that were *queued*.
	// Real system needs separate Tx status tracking using the Tx hash obtained from broadcast result.
	return newOrderCids, nil // Return Client IDs as placeholder exchange IDs
}


// Close cleans up the Injective client resources.
func (c *Client) Close() error {
	c.logger.Info("Injective client shutting down...")

	// Cancel the client's context FIRST. This signals cancellation
	// to any goroutines started by this client (like internal SDK queue processors).
	c.cancel()

	// --- Close Injective SDK connections ---
	if c.chainClient != nil {
		c.logger.Info("Closing Injective chain client...")
		c.chainClient.Close() // Call Close without capturing a return value
		c.chainClient = nil // Mark as nil after closing
		c.logger.Info("Injective chain client closed.")
	}


	
	
	time.Sleep(time.Millisecond * 500) // Allow queued messages a moment (adjust as needed)


	c.logger.Info("Injective client shut down complete.")

	// Return any errors encountered during other cleanup steps if applicable.
	// Since c.chainClient.Close() doesn't return an error, we just return nil here.
	return nil
}

// Add this line at the end of the file to ensure it implements the interface
var _ExchangeClient = (*Client)(nil) // Correct type reference