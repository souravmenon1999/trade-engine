package injective

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"net/http"
	"time"

	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	projecttypes "github.com/souravmenon1999/trade-engine/internal/types"
	"log/slog"
	"math/big"

	"github.com/shopspring/decimal"
	"github.com/google/uuid"
	"github.com/InjectiveLabs/sdk-go/client/common"
	chainclient "github.com/InjectiveLabs/sdk-go/client/chain"
	exchangetypes "github.com/InjectiveLabs/sdk-go/chain/exchange/types"
	"cosmossdk.io/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/btcsuite/btcutil/bech32"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"google.golang.org/grpc"
)

// Client implements the ExchangeClient interface for Injective.
type Client struct {
	cfg            *config.InjectiveConfig
	logger         *slog.Logger
	httpClient     *http.Client
	accountAddress string
	subaccountID   string
	ctx            context.Context
	cancel         context.CancelFunc
	orderbook      *projecttypes.Orderbook
	privKey        *ecdsa.PrivateKey
	chainID        string
}

// NewClient creates a new Injective client.
func NewClient(ctx context.Context, cfg *config.InjectiveConfig) (*Client, error) {
	clientCtx, cancel := context.WithCancel(ctx)

	client := &Client{
		cfg:        cfg,
		logger:     logging.GetLogger().With("exchange", "injective"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ctx:        clientCtx,
		cancel:     cancel,
		orderbook:  nil,
	}

	// Load Network configuration
	network := common.LoadNetwork(cfg.ChainConfig, "lb")
	client.chainID = network.ChainId

	// Derive address from private key
	privKey, err := crypto.HexToECDSA(cfg.PrivateKey)
	if err != nil {
		client.logger.Error("Failed to parse private key", "error", err)
		return nil, projecttypes.TradingError{
			Code:    projecttypes.ErrConnectionFailed,
			Message: "Failed to parse private key",
			Wrapped: err,
		}
	}
	client.privKey = privKey

	// Derive Bech32 address (inj prefix)
	pubKey := privKey.PublicKey
	addrBytes := crypto.PubkeyToAddress(pubKey).Bytes()
	converted, err := bech32.ConvertBits(addrBytes, 8, 5, true)
	if err != nil {
		client.logger.Error("Failed to convert address bytes for Bech32", "error", err)
		return nil, projecttypes.TradingError{
			Code:    projecttypes.ErrConnectionFailed,
			Message: "Failed to convert address for Bech32 encoding",
			Wrapped: err,
		}
	}
	accAddress, err := bech32.Encode("inj", converted)
	if err != nil {
		client.logger.Error("Failed to encode Bech32 address", "error", err)
		return nil, projecttypes.TradingError{
			Code:    projecttypes.ErrConnectionFailed,
			Message: "Failed to encode Bech32 address",
			Wrapped: err,
		}
	}
	client.accountAddress = accAddress
	client.logger.Info("Injective wallet initialized", "address", accAddress)

	// Derive Default Subaccount ID
	client.subaccountID = deriveSubaccountID(accAddress)
	client.logger.Info("Using default subaccount", "subaccount_id", client.subaccountID)

	client.logger.Info("Injective client initialization complete.")
	return client, nil
}

// deriveSubaccountID derives the default subaccount ID from the account address.
func deriveSubaccountID(addr string) string {
	return fmt.Sprintf("%s000000000000000000000000", addr)
}

// GetExchangeType returns the type of this exchange client.
func (c *Client) GetExchangeType() projecttypes.ExchangeType {
	return projecttypes.ExchangeInjective
}

// GetOrderbook returns nil for the Injective client in this setup.
func (c *Client) GetOrderbook() *projecttypes.Orderbook {
	return c.orderbook
}

// SubscribeOrderbook is not implemented for the Injective trading client.
func (c *Client) SubscribeOrderbook(ctx context.Context, symbol string) error {
	c.logger.Warn("SubscribeOrderbook is not implemented for the Injective client.")
	return nil
}

// translateOrderDataToSpotLimitOrderData converts a projecttypes.Order to an Injective SpotOrder.
func (c *Client) translateOrderDataToSpotLimitOrderData(
	order projecttypes.Order,
	senderAddress string,
	marketID string,
) (*exchangetypes.SpotOrder, error) {
	if order.Exchange != projecttypes.ExchangeInjective {
		return nil, fmt.Errorf("order is not for Injective")
	}
	if order.Instrument == nil {
		return nil, fmt.Errorf("order is missing instrument details")
	}

	// Simplified scaling (replace with market metadata in production)
	priceDecimal := decimal.NewFromBigInt(new(big.Int).SetUint64(order.Price), 0)
	quantityDecimal := decimal.NewFromBigInt(new(big.Int).SetUint64(order.Quantity), 0)

	// Convert to math.LegacyDec
	priceLegacyDec, err := math.LegacyNewDecFromStr(priceDecimal.String())
	if err != nil {
		return nil, fmt.Errorf("failed to convert price to LegacyDec: %w", err)
	}
	quantityLegacyDec, err := math.LegacyNewDecFromStr(quantityDecimal.String())
	if err != nil {
		return nil, fmt.Errorf("failed to convert quantity to LegacyDec: %w", err)
	}

	// Determine Injective OrderType
	injOrderType := exchangetypes.OrderType_BUY
	switch order.Side {
	case projecttypes.Buy:
		injOrderType = exchangetypes.OrderType_BUY
	case projecttypes.Sell:
		injOrderType = exchangetypes.OrderType_SELL
	default:
		return nil, fmt.Errorf("unsupported order side for Injective: %s", order.Side)
	}

	clientOrderID := uuid.New().String()

	return &exchangetypes.SpotOrder{
		MarketId: marketID,
		OrderInfo: exchangetypes.OrderInfo{
			SubaccountId: c.subaccountID,
			FeeRecipient: senderAddress,
			Price:        priceLegacyDec,
			Quantity:     quantityLegacyDec,
			Cid:          clientOrderID,
		},
		OrderType: injOrderType,
	}, nil
}

// SubmitOrder sends a single order to the Injective chain.
func (c *Client) SubmitOrder(ctx context.Context, order projecttypes.Order) (string, error) {
	if order.Exchange != projecttypes.ExchangeInjective {
		return "", projecttypes.TradingError{
			Code:    projecttypes.ErrOrderSubmissionFailed,
			Message: "Order is not intended for Injective",
			Wrapped: fmt.Errorf("order target exchange: %s", order.Exchange),
		}
	}

	marketID := c.cfg.MarketID
	c.logger.Info("Submitting single order to Injective...", "order", order, "market_id", marketID)

	spotOrder, err := c.translateOrderDataToSpotLimitOrderData(
		order,
		c.accountAddress,
		marketID,
	)
	if err != nil {
		c.logger.Error("Failed to translate single order data", "error", err)
		return "", projecttypes.TradingError{
			Code:    projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to translate order data",
			Wrapped: err,
		}
	}

	msg := createBatchUpdateMessage(
		c.accountAddress,
		c.subaccountID,
		nil,
		nil,
		[]*exchangetypes.SpotOrder{spotOrder},
		nil,
	)

	txHash, err := c.broadcastTxViaREST(ctx, msg)
	if err != nil {
		c.logger.Error("Failed to broadcast single order", "error", err, "order", order, "market_id", marketID)
		return "", projecttypes.TradingError{
			Code:    projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to broadcast transaction for single order",
			Wrapped: err,
		}
	}

	c.logger.Info("Single order broadcast completed", "order", order, "client_order_id", spotOrder.OrderInfo.Cid, "market_id", marketID, "txhash", txHash)
	return spotOrder.OrderInfo.Cid, nil
}

// CancelAllOrders cancels all open orders for the configured market and subaccount.
func (c *Client) CancelAllOrders(ctx context.Context, instrument *projecttypes.Instrument) error {
	if instrument == nil {
		return fmt.Errorf("instrument is nil")
	}
	marketID := c.cfg.MarketID
	c.logger.Info("Attempting to cancel all orders for market...", "market_id", marketID, "subaccount_id", c.subaccountID)

	msg := createBatchUpdateMessage(
		c.accountAddress,
		c.subaccountID,
		[]string{marketID},
		nil,
		nil,
		nil,
	)

	txHash, err := c.broadcastTxViaREST(ctx, msg)
	if err != nil {
		c.logger.Error("Failed to broadcast cancel all orders", "error", err, "market_id", marketID)
		return projecttypes.TradingError{
			Code:    projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to broadcast transaction for cancelling all orders",
			Wrapped: err,
		}
	}

	c.logger.Info("Cancel all orders broadcast completed", "market_id", marketID, "txhash", txHash)
	return nil
}

// ReplaceQuotes atomically cancels existing quotes and places new orders.
func (c *Client) ReplaceQuotes(ctx context.Context, instrument *projecttypes.Instrument, ordersToPlace []*projecttypes.Order) ([]string, error) {
	if instrument == nil {
		return nil, fmt.Errorf("instrument is nil")
	}
	marketID := c.cfg.MarketID

	if len(ordersToPlace) == 0 {
		c.logger.Info("ReplaceQuotes called with no orders to place. Will only cancel existing.", "market_id", marketID)
	}

	c.logger.Info("Attempting to replace quotes...",
		"market_id", marketID, "subaccount_id", c.subaccountID, "orders_to_place_count", len(ordersToPlace))

	spotOrdersToCreate := make([]*exchangetypes.SpotOrder, 0, len(ordersToPlace))
	newOrderCids := []string{}

	for _, order := range ordersToPlace {
		if order == nil {
			c.logger.Warn("Skipping nil order in ReplaceQuotes list")
			continue
		}
		if order.Exchange != projecttypes.ExchangeInjective {
			c.logger.Error("Skipping order not intended for Injective in ReplaceQuotes list", "order", order)
			continue
		}

		spotOrder, err := c.translateOrderDataToSpotLimitOrderData(
			*order,
			c.accountAddress,
			marketID,
		)
		if err != nil {
			c.logger.Error("Failed to translate order data for batch update", "error", err, "order", order)
			continue
		}

		spotOrdersToCreate = append(spotOrdersToCreate, spotOrder)
		newOrderCids = append(newOrderCids, spotOrder.OrderInfo.Cid)
	}

	if len(spotOrdersToCreate) == 0 && len(ordersToPlace) > 0 {
		return nil, projecttypes.TradingError{
			Code:    projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to translate/create any valid orders for batch update",
		}
	}

	msg := createBatchUpdateMessage(
		c.accountAddress,
		c.subaccountID,
		[]string{marketID},
		nil,
		spotOrdersToCreate,
		nil,
	)

	txHash, err := c.broadcastTxViaREST(ctx, msg)
	if err != nil {
		c.logger.Error("Failed to broadcast batch update", "error", err, "market_id", marketID, "orders_count", len(spotOrdersToCreate))
		return nil, projecttypes.TradingError{
			Code:    projecttypes.ErrOrderSubmissionFailed,
			Message: "Failed to broadcast transaction for batch order update",
			Wrapped: err,
		}
	}

	c.logger.Info("Batch update (cancel all + place new) broadcast completed",
		"market_id", marketID, "orders_broadcast_count", len(spotOrdersToCreate), "new_order_cids", newOrderCids, "txhash", txHash)
	return newOrderCids, nil
}

// broadcastTxViaREST broadcasts a transaction via Injective's Chain Client.
func (c *Client) broadcastTxViaREST(ctx context.Context, msg sdk.Msg) (string, error) {
	c.logger.Info("Preparing to broadcast transaction via Injective Chain Client")

	// Load network
	network := common.LoadNetwork(c.cfg.ChainConfig, "lb")

	// Create codec for keyring
	registry := codectypes.NewInterfaceRegistry()
	exchangetypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	// Create keyring
	kr, err := keyring.New(
		"injective",
		keyring.BackendMemory,
		"",
		nil,
		cdc,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create keyring: %w", err)
	}

	// Import private key into keyring
	_, mnemonic, err := kr.NewMnemonic("default", keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		return "", fmt.Errorf("failed to create keyring record: %w", err)
	}
	_ = mnemonic // Ignore mnemonic if not needed

	// Create gRPC connection
	grpcConn, err := grpc.Dial(network.ChainGrpcEndpoint, grpc.WithInsecure())
	if err != nil {
		return "", fmt.Errorf("failed to dial gRPC: %w", err)
	}
	defer grpcConn.Close()

	// Create client context
	clientCtx := client.Context{}.
		WithChainID(network.ChainId).
		WithKeyring(kr).
		WithFromAddress(sdk.MustAccAddressFromBech32(c.accountAddress)).
		WithFromName("default").
		WithNodeURI(network.TmEndpoint).
		WithCodec(cdc).
		WithInterfaceRegistry(registry)

	// Create chain client
	chainClient, err := chainclient.NewChainClient(clientCtx, network, common.OptionGasPrices("0.0005inj"))
	if err != nil {
		return "", fmt.Errorf("failed to create chain client: %w", err)
	}
	defer chainClient.Close()

	c.logger.Info("Chain client created, preparing transaction")

	// Broadcast transaction
	txResp, err := chainClient.SyncBroadcastMsg(msg)
	if err != nil {
		c.logger.Error("Failed to broadcast transaction", "error", err)
		return "", fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	txHash := txResp.TxResponse.TxHash
	c.logger.Info("Transaction broadcast successful", "txhash", txHash)
	return txHash, nil
}

// Close cleans up the Injective client resources.
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	c.cancel()
	c.logger.Info("Injective client shutting down...")
	time.Sleep(1 * time.Second)
	c.logger.Info("Injective client shut down complete.")
	return nil
}

// Ensure ExchangeClient interface is implemented
var _ExchangeClient = (*Client)(nil)