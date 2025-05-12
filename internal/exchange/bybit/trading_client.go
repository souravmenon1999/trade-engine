// internal/exchange/bybit/trading_client.go - Bybit Client for Trading Only
package bybit

import (
	"bytes" // Required for http.NewRequestWithContext body
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors" // Ensure errors is imported
	"fmt"
	"io"
	"math"
	"math/big" // Required by decimal.NewFromBigInt
	"net/http"
	"strconv"
	"strings" // Ensure strings is imported
	"sync"
	"sync/atomic" // Keep atomic for types.Instrument
	"time"
"net/url"
	"github.com/souravmenon1999/trade-engine/internal/config"
	"github.com/souravmenon1999/trade-engine/internal/logging"
	"github.com/souravmenon1999/trade-engine/internal/types"
	"github.com/souravmenon1999/trade-engine/internal/exchange" // Import the exchange package for the interface check
	"log/slog"
	"github.com/google/uuid" // Required for Client Order IDs
    "github.com/shopspring/decimal" // Required for precise decimal handling

)

// Note: WSMessage, OrderbookData, WSSubscribe, BybitAPIResponse,
// PlaceOrderRequest, PlaceOrderResponse, CancelOrderRequest, CancelOrderResponse,
// AmendOrderRequest, AmendOrderResponse, CancelAllOrdersRequest, CancelAllOrdersResponse
// are assumed to be defined in internal/exchange/bybit/messages.go.
// The WSConnection interface and its methods are assumed to be defined in ws.go.


// TradingClient implements the ExchangeClient interface for Bybit,
// focusing only on trading operations via REST API.
// Data feed methods are implemented as dummies or return empty values.
type TradingClient struct { // Keeping the name as TradingClient
	cfg          *config.BybitConfig // Configuration including API keys/secrets and TradingURL (ASSUMES THESE FIELDS ARE NOW IN config.BybitConfig)
	instrument   *types.Instrument // The instrument we are trading (populated from market info)
	httpClient   *http.Client      // HTTP client for REST API trading
	logger       *slog.Logger

	// Context for the client's operations
	ctx context.Context
	cancel context.CancelFunc

	// Need market info for precise price/quantity scaling in trading methods
	marketInfoMu sync.RWMutex // Protects marketInfo access
	marketInfo map[string]*BybitMarketInfo // Map[symbol] -> MarketInfo

    // Store the trading category, now assuming it's in config.BybitConfig
    // tradingCategory string <-- REMOVED, GET FROM cfg.Category

	// Optional: Add fields for managing trading state if needed (e.g., open orders cache)
	// openOrders sync.Map // Map[string (ClientOrderID)]*types.Order // Cache of orders placed by this client
}

// BybitMarketInfo holds details needed for precise scaling and order validation.
// This structure maps to the relevant fields from /v5/market/instruments-info.
// Corrected nested struct definitions to match expected JSON parsing if needed.
type BybitMarketInfo struct {
    Symbol string `json:"symbol"`
    PriceScale int // Number of decimal places for price (derived from PriceFilter.tickSize or API)
    QuantityScale int // Number of decimal places for quantity (derived from LotSizeFilter.qtyStep or API)
    MinQuantity decimal.Decimal
    MaxQuantity decimal.Decimal
    PriceFilter struct {
        MinPrice decimal.Decimal `json:"minPrice"` // Added json tag
        MaxPrice decimal.Decimal `json:"maxPrice"` // Added json tag
        TickSize decimal.Decimal `json:"tickSize"` // Added json tag
    }
    LotSizeFilter struct {
        MinQuantity decimal.Decimal `json:"minOrderQty"` // Added json tag
        MaxQuantity decimal.Decimal `json:"maxOrderQty"` // Added json tag
        QuantityStep decimal.Decimal `json:"qtyStep"` // Added json tag
    }
    BaseCoin string `json:"baseCoin"` // Base currency
    QuoteCoin string `json:"quoteCoin"` // Quote currency
    Status string `json:"status"` // Trading status, e.g., "Trading"
    // Add other relevant fields if needed
}


// NewTradingClient creates a new Bybit trading-only client.
// It requires a parent context for cancellation and the config.
// Assumes config.BybitConfig now contains TradingURL, APIKey, APISecret, Category.
func NewTradingClient(ctx context.Context, cfg *config.BybitConfig) (*TradingClient, error) { // Signature updated
	// Instrument details will be populated from fetched market info
	instrument := &types.Instrument{
		Symbol: cfg.Symbol, // Get symbol from config
		BaseCurrency: types.Currency(""), // Populated from market info
		QuoteCurrency: types.Currency(""), // Populated from market info
		MinLotSize: atomic.Uint64{}, // Populated from market info (scaled)
		ContractType: types.ContractTypeUnknown, // Populated from market info if available
	}

	clientCtx, cancel := context.WithCancel(ctx)

	client := &TradingClient{ // Return a pointer to TradingClient
		cfg:        cfg, // Store the config pointer
		instrument: instrument, // Store the instrument
		httpClient: &http.Client{Timeout: 15 * time.Second}, // HTTP client for REST
		logger:     logging.GetLogger().With("exchange", "bybit_trading"),
		ctx:        clientCtx,
		cancel:     cancel,
        marketInfo: make(map[string]*BybitMarketInfo),
        // tradingCategory: tradingCategory, <-- REMOVED
	}

    // Fetch market info on startup (essential for scaling price/quantity)
    // Use values directly from cfg
    if client.cfg.TradingURL == "" || client.cfg.Category == "" {
        client.logger.Warn("Bybit Trading URL or Category not configured. Trading methods will likely fail.")
    }

    // Use client.ctx for the context passed to fetchMarketInfo
    // Pass the category from config
    if err := client.fetchMarketInfo(client.ctx, cfg.Symbol, cfg.Category); err != nil {
        client.logger.Error("Failed to fetch market info on startup. Trading functionality may be impaired.", "error", err, "symbol", cfg.Symbol)
        // Decide if this is a fatal error or a warning. For now, log and continue.
        // Trading methods will check for market info availability.
    } else {
        client.logger.Info("Successfully fetched market info", "symbol", cfg.Symbol)
         // Update the instrument details from fetched info
         info := client.getMarketInfo(cfg.Symbol)
         if info != nil {
             client.instrument.BaseCurrency = types.Currency(info.BaseCoin)
             client.instrument.QuoteCurrency = types.Currency(info.QuoteCoin)
             // Update MinLotSize if your types.Instrument uses scaled integer
             // Use Float64() instead of InexactFloat64() and use the first return value
             minQtyFloat, _ := info.MinQuantity.Mul(decimal.NewFromInt(1e6)).Float64()
             minQtyScaled := uint64(math.Round(minQtyFloat)) // Use math.Round with single float64
             client.instrument.MinLotSize.Store(minQtyScaled) // Use the calculated value
             // minQtyScaled variable is now used, resolving the "declared and not used" warning.
             client.logger.Info("Updated instrument details from market info",
                 "base", client.instrument.BaseCurrency,
                 "quote", client.instrument.QuoteCurrency,
                 "min_qty_scaled", client.instrument.MinLotSize.Load(),
             )
         } else {
             client.logger.Warn("Market info fetched but not found for configured symbol after fetching.")
         }
    }


	return client, nil
}

// --- Dummy Data Feed Methods to satisfy ExchangeClient interface ---

// SubscribeOrderbook is not implemented for the Bybit trading client.
func (c *TradingClient) SubscribeOrderbook(ctx context.Context, symbol string) error {
	c.logger.Warn("SubscribeOrderbook is not implemented for the Bybit trading client.")
	return fmt.Errorf("SubscribeOrderbook not supported by Bybit trading client")
}

// GetOrderbook returns nil for the Bybit trading client.
func (c *TradingClient) GetOrderbook() *types.Orderbook {
	c.logger.Debug("GetOrderbook called on Bybit trading client, returning nil.")
	return nil // Trading client doesn't maintain a live orderbook
}

// --- End Dummy Data Feed Methods ---


// GetExchangeType returns the type of this exchange client.
func (c *TradingClient) GetExchangeType() types.ExchangeType {
	return types.ExchangeBybit
}


// fetchMarketInfo retrieves market details from Bybit REST API.
// Uses the public endpoint /v5/market/instruments-info.
// Accepts tradingCategory as a parameter.
func (c *TradingClient) fetchMarketInfo(ctx context.Context, symbol string, category string) error {
    if c.cfg.TradingURL == "" { // Use cfg
        return errors.New("Bybit Trading URL not configured, cannot fetch market info")
    }

    // Endpoint: GET /v5/market/instruments-info
    // Category is required, e.g., "linear", "inverse", "spot"
    // category is passed as a parameter

    endpoint := "/v5/market/instruments-info"
    requestURL := c.cfg.TradingURL + endpoint // Renamed variable from 'url' to 'requestURL'

    // Query parameters
    params := url.Values{} // Ensure url.Values is correctly recognized from net/url import
    params.Add("category", category)
    if symbol != "" {
        params.Add("symbol", symbol) // Fetch info for a specific symbol
    }

    fullURLStr := requestURL + "?" + params.Encode() // Renamed variable from 'fullURL'

    // Create the HTTP request (no signing needed for this public endpoint)
    req, err := http.NewRequestWithContext(ctx, "GET", fullURLStr, nil) // Use fullURLStr
    if err != nil {
        c.logger.Error("Failed to create fetch market info HTTP request", "error", err)
        return fmt.Errorf("failed to create HTTP request: %w", err)
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        c.logger.Error("Fetch market info HTTP request failed", "error", err)
        return fmt.Errorf("Bybit API request failed: %w", err)
    }
    defer resp.Body.Close()

    bodyBytes, err := io.ReadAll(resp.Body)
    if err != nil {
        c.logger.Error("Failed to read fetch market info response body", "error", err)
        return fmt.Errorf("failed to read API response body: %w", err)
    }

     if resp.StatusCode != http.StatusOK {
        c.logger.Error("Bybit API returned non-OK status for market info", "status", resp.StatusCode, "body", string(bodyBytes))
        // Attempt to parse as common API response to get retCode/retMsg
        var commonResp BybitAPIResponse // Assumes BybitAPIResponse is in messages.go
         if jsonErr := json.Unmarshal(bodyBytes, &commonResp); jsonErr == nil {
             return fmt.Errorf("Bybit API returned status %d: %s (RetCode %d)", resp.StatusCode, commonResp.RetMsg, commonResp.RetCode)
         }
        return fmt.Errorf("Bybit API returned status %d, failed to parse body", resp.StatusCode)
    }


    // Parse the successful response
    // Declare apiResp using the struct from messages.go
    var apiResp BybitInstrumentsInfoResponse // Assuming you define this struct in messages.go
    if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
        c.logger.Error("Failed to unmarshal fetch market info response", "error", err, "body", string(bodyBytes))
        return fmt.Errorf("failed to parse API response: %w", err)
    }

     if !apiResp.IsSuccess() { // Use IsSuccess method from BybitInstrumentsInfoResponse
        c.logger.Error("Bybit API returned error for fetch market info", "api_error", apiResp.Error()) // Use Error method
        return fmt.Errorf("Bybit API error: %w", apiResp)
    }

    // Store the relevant market info
    c.marketInfoMu.Lock()
    defer c.marketInfoMu.Unlock()

   
    // This loop iterates through the list from the API response (apiResp.Result.List)
    for _, item := range apiResp.Result.List {
        // Parse decimal string fields
        minQty, err := decimal.NewFromString(item.LotSizeFilter.MinQuantity)
        if err != nil { c.logger.Warn("Failed to parse minOrderQty", "error", err, "symbol", item.Symbol); continue }
        maxQty, err := decimal.NewFromString(item.LotSizeFilter.MaxQuantity) // FIXED: Changed MaxOrderQty to MaxQuantity
        if err != nil { c.logger.Warn("Failed to parse maxOrderQty", "error", err, "symbol", item.Symbol); continue }
        qtyStep, err := decimal.NewFromString(item.LotSizeFilter.QuantityStep)
        if err != nil { c.logger.Warn("Failed to parse qtyStep", "error", err, "symbol", item.Symbol); continue }
        minPrice, err := decimal.NewFromString(item.PriceFilter.MinPrice)
        if err != nil { c.logger.Warn("Failed to parse minPrice", "error", err, "symbol", item.Symbol); continue }
        maxPrice, err := decimal.NewFromString(item.PriceFilter.MaxPrice)
        if err != nil { c.logger.Warn("Failed to parse maxPrice", "error", err, "symbol", item.Symbol); continue }
        tickSize, err := decimal.NewFromString(item.PriceFilter.TickSize)
        if err != nil { c.logger.Warn("Failed to parse tickSize", "error", err, "symbol", item.Symbol); continue }

        // Determine PriceScale (number of decimals) from TickSize or PriceFilter rules
        // If TickSize is 0.0001, PriceScale is 4. If TickSize is 0.5, PriceScale is 1.
        // Count decimal places in tick size string representation.
        priceScale := 0
        tickSizeStr := tickSize.String()
        if strings.Contains(tickSizeStr, ".") {
             priceScale = len(strings.Split(tickSizeStr, ".")[1])
        }


         // Determine QuantityScale (number of decimals) from QtyStep or LotSizeFilter rules
        quantityScale := 0
        qtyStepStr := qtyStep.String()
         if strings.Contains(qtyStepStr, ".") {
             quantityScale = len(strings.Split(qtyStepStr, ".")[1])
         }

        // Corrected struct literal to match BybitMarketInfo fields and use NAMED nested struct types
        c.marketInfo[item.Symbol] = &BybitMarketInfo{
            Symbol: item.Symbol,
            Status: item.Status,
            BaseCoin: item.BaseCoin,
            QuoteCoin: item.QuoteCoin,
            PriceScale: priceScale, // Store derived scale
            QuantityScale: quantityScale, // Store derived scale
            MinQuantity: minQty, // Store parsed decimals
            MaxQuantity: maxQty, // Store parsed decimals
            PriceFilter: BybitPriceFilter{MinPrice: minPrice, MaxPrice: maxPrice, TickSize: tickSize}, // Use named type
            LotSizeFilter: BybitLotSizeFilter{MinQuantity: minQty, MaxQuantity: maxQty, QuantityStep: qtyStep}, // Use named type
        }
        if item.Symbol == symbol {
            
            c.logger.Info("Found market info for configured symbol", "symbol", symbol, "status", item.Status)
        }
    }

    // Validate trading status for the configured symbol
    if symbol != "" {
        info := c.getMarketInfo(symbol)
        if info != nil && info.Status != "Trading" {
             c.logger.Warn("Market is not in 'Trading' status", "symbol", symbol, "status", info.Status)
             // Decide if this should be an error or just a warning.
             // Returning error here would make init fail if market is not trading.
             // return fmt.Errorf("market %s is not in trading status: %s", symbol, info.Status)
        }
    }

    // Ensure a return statement exists at the end of the function's successful path
    return nil
}

// getMarketInfo safely retrieves stored market information by symbol.
func (c *TradingClient) getMarketInfo(symbol string) *BybitMarketInfo {
    c.marketInfoMu.RLock()
    defer c.marketInfoMu.RUnlock()
    info, ok := c.marketInfo[symbol]
    if !ok {
        return nil // Info not found
    }
    return info
}


// formatPrice formats a scaled uint64 price to a string with correct precision based on market info.
func (c *TradingClient) formatPrice(scaledPrice uint64, symbol string) (string, error) {
    info := c.getMarketInfo(symbol)
    if info == nil {
        return "", fmt.Errorf("market info not found for symbol %s to format price", symbol)
    }
    // Convert our scaled uint64 (1e6) back to decimal
    priceDecimal := decimal.NewFromBigInt(new(big.Int).SetUint64(scaledPrice), -6) // Use decimal with negative exponent

    // Use market info's TickSize for rounding
    // Rounded price = Round(price / tickSize) * tickSize
    // Ensure tickSize is not zero before dividing
    if info.PriceFilter.TickSize.Cmp(decimal.Zero) != 0 {
        priceDecimal = priceDecimal.Div(info.PriceFilter.TickSize).Round(0).Mul(info.PriceFilter.TickSize)
    } else {
        // Fallback to using PriceScale decimal places if tick size is zero
        c.logger.Warn("Market TickSize is zero, formatting price using PriceScale decimals", "symbol", symbol, "price_scale", info.PriceScale)
        priceDecimal = priceDecimal.Round(int32(info.PriceScale)) // Use Round instead of RoundFloor    
    }

    // Format as string with the required precision (usually matches TickSize decimals or PriceScale)
    // Using String() or StringFixed() depends on Bybit's exact requirement. String() is often sufficient after rounding.
    return priceDecimal.String(), nil
}

// formatQuantity formats a scaled uint64 quantity to a string with correct precision based on market info.
func (c *TradingClient) formatQuantity(scaledQuantity uint64, symbol string) (string, error) {
	info := c.getMarketInfo(symbol)
	if info == nil {
		return "", fmt.Errorf("market info not found for symbol %s to format quantity", symbol)
	}

	// Convert our scaled uint64 (1e6) back to decimal FIRST
	// Use decimal with negative exponent -6 for 1e6 scaling
	quantityDecimal := decimal.NewFromBigInt(new(big.Int).SetUint64(scaledQuantity), -6)

	// Use market info's QuantityStep for rounding
	// Rounded quantity = Round(quantity / qtyStep) * qtyStep
	// Ensure qtyStep is not zero before dividing
	if info.LotSizeFilter.QuantityStep.Cmp(decimal.Zero) != 0 {
		// Round to the nearest multiple of QuantityStep
		quantityDecimal = quantityDecimal.Div(info.LotSizeFilter.QuantityStep).Round(0).Mul(info.LotSizeFilter.QuantityStep)
	} else {
		// Fallback to using QuantityScale decimal places if qtyStep is zero
		c.logger.Warn("Market QuantityStep is zero, formatting quantity using QuantityScale decimals", "symbol", symbol, "quantity_scale", info.QuantityScale)
		// Round to the specified number of decimal places
		quantityDecimal = quantityDecimal.Round(int32(info.QuantityScale))
	}

	// Format as string with the correct precision (String() should handle this after rounding)
	return quantityDecimal.String(), nil
}
// signRequest signs the Bybit V5 REST API request payload.
// This is a crucial and complex part - refer to Bybit documentation for exact steps.
// This implementation is a SIMPLIFIED PLACEHOLDER and LIKELY INCORRECT for production.
// Bybit V5 signing typically involves:
// timestamp (ms) + apiKey + recvWindow + sorted_query_string_OR_body_string
func (c *TradingClient) signRequest(timestamp, apiKey, recvWindow string, body []byte) (string, error) {
     if c.cfg.APISecret == "" { // Use cfg.APISecret
         return "", fmt.Errorf("API secret is not configured for signing")
     }
    // For POST requests with JSON body, the payload string in the signature is the JSON body as a string.
    // For GET requests, it's the sorted query string.

    // Simplified example for POST body:
    // The string to sign is timestamp + apiKey + recvWindow + body (as string)
    // For GET, it's timestamp + apiKey + recvWindow + sorted_query_string
    // The sendBybitRequest helper constructs the stringToSign correctly.
    // This function just performs the HMAC calculation on the provided stringToSign.

    stringToSign := string(body) // The body here is actually the string to sign passed from sendBybitRequest

    // Create HMAC SHA256 signature
	h := hmac.New(sha256.New, []byte(c.cfg.APISecret)) // Use cfg.APISecret
	h.Write([]byte(stringToSign))
	signature := fmt.Sprintf("%x", h.Sum(nil))

	return signature, nil
}


// sendBybitRequest executes a signed HTTP request to the Bybit API.
// Handles common headers, signing, execution, and basic response parsing.
// Returns the raw response body bytes on success for the caller to unmarshal
// into the endpoint-specific struct, or a TradingError on failure.
func (c *TradingClient) sendBybitRequest(ctx context.Context, method, endpoint string, queryParams url.Values, body interface{}) ([]byte, error) {
    if c.cfg.APIKey == "" || c.cfg.APISecret == "" || c.cfg.TradingURL == "" { // Use cfg fields
       return nil, types.TradingError{
           Code: types.ErrConfigLoading,
           Message: "Bybit API trading config incomplete (key, secret, or url)",
       }
   }
   if c.cfg.TradingURL == "" { // Use cfg.TradingURL
        return nil, types.TradingError{
           Code: types.ErrConfigLoading,
           Message: "Bybit Trading URL is not configured",
       }
   }

   // Declare reqBodyBytes here
   var reqBodyBytes []byte
   var err error
   if body != nil {
       // Marshal the request body if it exists
       reqBodyBytes, err = json.Marshal(body)
       if err != nil {
           c.logger.Error("Failed to marshal request body for Bybit API", "error", err, "method", method, "endpoint", endpoint)
           return nil, types.TradingError{
               Code: types.ErrUnknown,
               Message: "Failed to format request body",
               Wrapped: err,
           }
       }
   }

   // Construct the string to sign based on method and payload
   // For V5 POST with JSON body, sign timestamp + apiKey + recvWindow + json_body_string
   // For V5 GET, sign timestamp + apiKey + recvWindow + sorted_query_string
   // This needs precise implementation based on Bybit docs.
   var signPayload string
   if method == "POST" {
       signPayload = string(reqBodyBytes) // Payload is the JSON body as a string
   } else if method == "GET" {
        signPayload = queryParams.Encode() // Payload is the sorted query string
        // reqBodyBytes = nil // No body in signing for GET, but we need it for the request itself
   } else {
        // Handle other methods like DELETE if needed
        return nil, fmt.Errorf("unsupported HTTP method for Bybit signing: %s", method)
   }

   // Add required headers for signing
   timestamp := strconv.FormatInt(time.Now().UnixNano()/1e6, 10) // Milliseconds timestamp string
   recvWindow := "5000" // Time in ms payload is valid (adjust as needed)

   // Build the complete string to sign
   stringToSign := timestamp + c.cfg.APIKey + recvWindow + signPayload // Use cfg.APIKey

   // Create the signature using HMAC SHA256
   signature, err := c.signRequest(timestamp, c.cfg.APIKey, recvWindow, []byte(stringToSign)) // Pass the stringToSign as bytes, use cfg.APIKey
    if err != nil {
        c.logger.Error("Failed to sign Bybit API request", "error", err, "method", method, "endpoint", endpoint)
         return nil, types.TradingError{
           Code: types.ErrUnknown, // Or ErrAuthenticationFailed
           Message: "Failed to sign API request",
           Wrapped: err,
       }
    }


   // Construct full URL with query parameters
   fullURLStr := c.cfg.TradingURL + endpoint // Use cfg.TradingURL
   // Query params are usually sorted alphabetically by key for signing, url.Values.Encode() does this.
   if len(queryParams) > 0 {
       fullURLStr = fullURLStr + "?" + queryParams.Encode()
   }


   // Create the HTTP request
   // Pass the request body reader (io.Reader) which bytes.NewBuffer implements
   // If it's a GET request, reqBodyBytes will be empty, which is fine for bytes.NewBuffer.
   req, err := http.NewRequestWithContext(ctx, method, fullURLStr, bytes.NewBuffer(reqBodyBytes))
   if err != nil {
       c.logger.Error("Failed to create Bybit API HTTP request", "error", err, "method", method, "endpoint", endpoint)
       return nil, types.TradingError{
           Code: types.ErrConnectionFailed, // Or ErrUnknown
           Message: "Failed to create HTTP request",
           Wrapped: err,
       }
   }


   // Add headers
   req.Header.Set("Content-Type", "application/json") // Common for V5 trading endpoints
   if method == "GET" {
        // Bybit GET requests might not strictly require Content-Type "application/json"
        // or might need "application/x-www-form-urlencoded". Check docs.
        // If sending query params in GET, Content-Type is often unnecessary or different.
        // Remove if not needed: req.Header.Set("Content-Type", "application/json")
   }

   req.Header.Set("X-BAPI-API-KEY", c.cfg.APIKey) // Use cfg.APIKey
   req.Header.Set("X-BAPI-TIMESTAMP", timestamp)
   req.Header.Set("X-BAPI-SIGN", signature)
   req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)


   // Execute the request
   resp, err := c.httpClient.Do(req)
   if err != nil {
       c.logger.Error("Bybit API HTTP request failed", "error", err, "method", method, "endpoint", endpoint)
       return nil, types.TradingError{
           Code: types.ErrConnectionFailed,
           Message: "Bybit API request failed",
           Wrapped: err,
       }
   }
   defer resp.Body.Close()

   // Read the response body
   // Declare bodyBytes here so it's accessible throughout the function
   bodyBytes, err := io.ReadAll(resp.Body) // This bodyBytes is different from reqBodyBytes
   if err != nil {
       c.logger.Error("Failed to read Bybit API response body", "error", err, "method", method, "endpoint", endpoint)
       return nil, types.TradingError{
           Code: types.ErrUnknown,
           Message: "Failed to read API response",
           Wrapped: err,
       }
   }

   // Parse the common API response structure
   var commonResp BybitAPIResponse // Assumes BybitAPIResponse is in messages.go
   if err := json.Unmarshal(bodyBytes, &commonResp); err != nil {
       c.logger.Error("Failed to unmarshal common Bybit API response structure", "error", err, "method", method, "endpoint", endpoint, "body", string(bodyBytes))
        // Return raw body if parsing fails, but wrap in error
       return nil, types.TradingError{
           Code: types.ErrUnknown,
           Message: "Failed to parse common API response structure",
           Wrapped: fmt.Errorf("raw body: %s", string(bodyBytes)),
       }
   }

   // Check for API-level error code (RetCode)
   if !commonResp.IsSuccess() { // Use IsSuccess method
       c.logger.Error("Bybit API returned error", "method", method, "endpoint", endpoint, "api_error", commonResp.Error(), "status", resp.StatusCode) // Use Error method
       // Map specific RetCode to our ErrorCode if needed
       errorCode := types.ErrOrderRejected // Default for trading errors
       // You might add a switch statement here to map specific Bybit RetCodes to types.ErrorCode
       // e.g., case 10001: errorCode = types.ErrInvalidParameter
       // case 30001: errorCode = types.ErrInsufficientBalance // Example

       return nil, types.TradingError{
           Code: errorCode,
           Message: fmt.Sprintf("Bybit API rejected request: %s", commonResp.RetMsg),
           Wrapped: fmt.Errorf("Bybit RetCode: %d", commonResp.RetCode),
       }
   }

   // If successful at the API level, return the raw body for the caller to unmarshal
   return bodyBytes, nil
}



// SubmitOrder sends a single order to Bybit.
// This uses the Bybit REST API endpoint /v5/order/create.
// It ignores the order.Exchange field because this client only trades on Bybit.
func (c *TradingClient) SubmitOrder(ctx context.Context, order types.Order) (string, error) {
    // Note: We ignore order.Exchange here due to the constraint on processor.ProcessOrderbook
    // which always returns types.ExchangeInjective. This client assumes it's trading on Bybit.

    if order.Instrument == nil {
         return "", fmt.Errorf("order is missing instrument details") // Return empty string
    }
     if c.cfg.APIKey == "" || c.cfg.APISecret == "" || c.cfg.TradingURL == "" { // Use cfg fields
        return "", types.TradingError{ // Return empty string
            Code: types.ErrConfigLoading,
            Message: "Bybit API trading config incomplete (key, secret, or url)",
        }
    }
     // Basic validation
     if order.Quantity == 0 || order.Price == 0 {
         return "", fmt.Errorf("order quantity or price cannot be zero") // Return empty string
     }


	c.logger.Info("Submitting single order to Bybit REST API...", "order", order)

	// Get market info for precise scaling
    info := c.getMarketInfo(order.Instrument.Symbol)
     if info == nil {
         return "", types.TradingError{ // Return empty string
             Code: types.ErrUnknown,
             Message: fmt.Sprintf("Market info not found for symbol %s, cannot submit order", order.Instrument.Symbol),
         }
     }
     if info.Status != "Trading" {
          return "", types.TradingError{ // Return empty string
             Code: types.ErrOrderRejected, // Or a more specific status code
             Message: fmt.Sprintf("Market %s is not in trading status (%s), cannot submit order", order.Instrument.Symbol, info.Status),
         }
     }


	// Convert scaled uint64 price/quantity to strings with correct precision using market info
	priceStr, priceErr := c.formatPrice(order.Price, order.Instrument.Symbol)
    if priceErr != nil {
        c.logger.Error("Failed to format price for Bybit submission", "error", priceErr, "order", order)
        return "", types.TradingError{ // Return empty string
            Code: types.ErrOrderSubmissionFailed,
            Message: "Failed to format price for API",
            Wrapped: priceErr,
        }
    }
    quantityStr, quantityErr := c.formatQuantity(order.Quantity, order.Instrument.Symbol)
    if quantityErr != nil {
         c.logger.Error("Failed to format quantity for Bybit submission", "error", quantityErr, "order", order)
         return "", types.TradingError{ // Return empty string
            Code: types.ErrOrderSubmissionFailed,
            Message: "Failed to format quantity for API",
            Wrapped: quantityErr,
        }
    }

    // Map standardized order side
	bybitSide := "Buy"
	if order.Side == types.Sell {
		bybitSide = "Sell"
	}

    // Generate unique Client Order ID (Cid)
    clientOrderID := uuid.New().String()


	reqBody := PlaceOrderRequest{ // Assumes PlaceOrderRequest is defined in messages.go
		Category: c.cfg.Category, // Use cfg.Category
		Symbol:   order.Instrument.Symbol,
		Side:     bybitSide,
		OrderType: "Limit", // Assuming limit orders for quoting
		Qty:      quantityStr, // Use formatted string quantity
		Price:    priceStr,    // Use formatted string price
		ClientOrderID: clientOrderID, // Send our generated client ID
		TimeInForce: "GTC", // Good 'Til Cancelled is common for quotes
		// Add other parameters as needed based on Bybit API docs and strategy
		// IsLeverage: 0, // For cross/isolated margin
		// OrderFilter: "Order", // For normal orders
	}

	// Execute the REST request using the helper
	endpoint := "/v5/order/create"
    // No specific query params for create order POST body

	rawBody, err := c.sendBybitRequest(ctx, "POST", endpoint, nil, reqBody)

	if err != nil {
		c.logger.Error("Bybit place order request failed", "error", err, "order", order)
        // sendBybitRequest already wraps errors in TradingError
		return "", err // Return empty string along with the error
	}

	// Parse the successful response body
	var apiResp PlaceOrderResponse // Assumes PlaceOrderResponse is defined in messages.go
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		c.logger.Error("Failed to unmarshal Bybit place order response body", "error", err, "body", string(rawBody))
		return "", types.TradingError{ // Return empty string
			Code: types.ErrUnknown,
			Message: "Failed to parse API response result for place order",
			Wrapped: err,
		}
	}

	c.logger.Info("Order submitted successfully to Bybit",
        "order", order,
        "exchange_order_id", apiResp.Result.OrderId,
        "client_order_id", apiResp.Result.ClientOrderId,
    )

	// Return the exchange's order ID
	return apiResp.Result.OrderId, nil
}



// cancelSingleOrder cancels a single open order by OrderId or ClientOrderId.
// This uses the Bybit REST API endpoint /v5/order/cancel.
// This is a private helper method for the Bybit client.
func (c *TradingClient) CancelSingleOrder(ctx context.Context, orderID string, symbol string) error { // Changed name to Capital C
    if orderID == "" {
        c.logger.Debug("CancelSingleOrder called with empty orderID, nothing to cancel") // Update log message
        return nil // Not an error, just nothing to do
    }
     if c.cfg.APIKey == "" || c.cfg.APISecret == "" || c.cfg.TradingURL == "" { // Use cfg fields
        return types.TradingError{
            Code: types.ErrConfigLoading,
            Message: "Bybit API trading config incomplete (key, secret, or url)",
        }
    }


	c.logger.Info("Attempting to cancel single order on Bybit REST API...", "order_id", orderID, "symbol", symbol)

	// Map standardized request to Bybit request
	reqBody := CancelOrderRequest{ // Use the struct defined in messages.go
		Category: c.cfg.Category, // Use cfg.Category
		Symbol:   symbol,
		OrderId:  orderID, // Cancel by Exchange Order ID
		// ClientOrderId: "", // Can use ClientOrderId too if tracked and preferred
	}

	// Execute the REST request using the helper
	endpoint := "/v5/order/cancel"
    // No specific query params for cancel POST body

	rawBody, err := c.sendBybitRequest(ctx, "POST", endpoint, nil, reqBody)

	if err != nil {
		c.logger.Error("Bybit cancel single order request failed", "error", err, "order_id", orderID)
		return err // sendBybitRequest already wraps errors
	}

	// Parse the successful response body (CancelOrderResponse)
	var apiResp CancelOrderResponse // Use the struct defined in messages.go
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		c.logger.Error("Failed to unmarshal Bybit cancel single order response body", "error", err, "order_id", orderID, "body", string(rawBody))
		return types.TradingError{
			Code: types.ErrUnknown,
			Message: "Failed to parse API response result for cancel single order",
			Wrapped: err,
		}
	}

	// Check the returned OrderId in the result to confirm which order was cancelled.
	// It should match the orderID we sent.
	if apiResp.Result.OrderId != orderID {
		// This might indicate the order was already cancelled or not found.
		// Depending on desired strictness, this could be treated as a soft error or just logged.
		c.logger.Warn("Bybit cancel single order response OrderId mismatch or order not found/already cancelled",
            "requested_id", orderID,
            "response_id", apiResp.Result.OrderId,
        )
        // Decide if mismatch constitutes an error. For now, if RetCode was 0, assume success or already gone.
        // If you need to distinguish "successfully cancelled" vs "already gone", check RetCode and RetMsg carefully.
	} else {
        c.logger.Info("Single order cancelled successfully on Bybit", "order_id", apiResp.Result.OrderId)
    }

    // Bybit API returns 0 RetCode even if the order was already cancelled.
    // We rely on sendBybitRequest to check RetCode. If we reach here, RetCode was 0.
    // A mismatch in returned Orderid usually means it was already gone.
    // Let's return nil error in this case too, as the desired state (order gone) is achieved.
    // If you need strict confirmation it *was* cancelled *by this request*, add more checks.

	return nil
}


// CancelAllOrders cancels all open orders for a given instrument on Bybit.
// This uses the Bybit REST API endpoint /v5/order/cancel-all.
func (c *TradingClient) CancelAllOrders(ctx context.Context, instrument *types.Instrument) error {
	if instrument == nil {
		return fmt.Errorf("instrument is nil")
	}
     if c.cfg.APIKey == "" || c.cfg.APISecret == "" || c.cfg.TradingURL == "" { // Use cfg fields
        return types.TradingError{
            Code: types.ErrConfigLoading,
            Message: "Bybit API trading config incomplete (key, secret, or url)",
        }
    }


	c.logger.Info("Attempting to cancel all orders on Bybit REST API...", "symbol", instrument.Symbol)

	// Map standardized request to Bybit request
	reqBody := CancelAllOrdersRequest{ // Use the struct defined in messages.go
		Category: c.cfg.Category, // Use cfg.Category
		Symbol:   instrument.Symbol,
	}

	// Execute the REST request using the helper
	endpoint := "/v5/order/cancel-all"
    // No specific query params for cancel-all POST body

	rawBody, err := c.sendBybitRequest(ctx, "POST", endpoint, nil, reqBody)

	if err != nil {
		c.logger.Error("Bybit cancel all orders request failed", "error", err, "symbol", instrument.Symbol)
        // sendBybitRequest already wraps errors in TradingError
		return err // Return the wrapped error
	}

	// Parse the successful response body (CancelAllOrdersResponse)
    // The actual structure might contain a list of orders that *were* cancelled.
	var apiResp CancelAllOrdersResponse // Use the struct defined in messages.go
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		c.logger.Error("Failed to unmarshal Bybit cancel all response body", "error", err, "body", string(rawBody))
		return types.TradingError{
			Code: types.ErrUnknown,
			Message: "Failed to parse API response result for cancel all",
			Wrapped: err,
		}
	}

	// Log the list of orders that the API attempted to cancel (from the result field)
	cancelledIDs := []string{}
	if apiResp.Result.List != nil { // Check if the list is not nil
		for _, item := range apiResp.Result.List {
			cancelledIDs = append(cancelledIDs, item.OrderId)
		}
	} else {
        c.logger.Debug("Cancel all successful, but result list is nil or empty.")
    }


	c.logger.Info("Cancel all orders request sent successfully to Bybit", "symbol", instrument.Symbol, "attempted_to_cancel", cancelledIDs)

	return nil // Success means the API accepted the cancel request
}

// ReplaceQuotes implements the ExchangeClient interface.
// For Bybit REST, this is typically implemented as CancelAll followed by individual Submit calls.
// This is NOT atomic and subject to race conditions.
// It ignores the order.Exchange field because this client only trades on Bybit.
func (c *TradingClient) ReplaceQuotes(ctx context.Context, instrument *types.Instrument, ordersToPlace []*types.Order) ([]string, error) {
    if instrument == nil {
		return nil, fmt.Errorf("instrument is nil")
	}
     if c.cfg.APIKey == "" || c.cfg.APISecret == "" || c.cfg.TradingURL == "" { // Use cfg fields
        return nil, types.TradingError{
            Code: types.ErrConfigLoading,
            Message: "Bybit API trading config incomplete (key, secret, or url)",
        }
    }

	c.logger.Warn("ReplaceQuotes called on Bybit trading client. Note: Bybit REST ReplaceQuotes is NOT atomic (CancelAll + individual Submit). Consider using AmendOrder directly in strategy for quotes.")
	c.logger.Info("Attempting to replace quotes on Bybit (non-atomic REST sequence)...",
		"symbol", instrument.Symbol, "orders_to_place_count", len(ordersToPlace))

	// Step 1: Cancel all existing orders for this symbol
	cancelErr := c.CancelAllOrders(ctx, instrument)
	if cancelErr != nil {
		c.logger.Error("Failed to send cancel all orders request during ReplaceQuotes", "error", cancelErr, "symbol", instrument.Symbol)
		return nil, types.TradingError{
			Code: types.ErrUnknown, // Use ErrUnknown as we cannot add ErrCancellationFailed
			Message: "Failed to cancel existing orders before placing new quotes",
			Wrapped: cancelErr,
		}
	}
	c.logger.Info("Cancel all orders request sent successfully during Bybit ReplaceQuotes.", "symbol", instrument.Symbol)


	// Step 2: Place the new orders individually
	placedOrderIDs := []string{} // Store the exchange IDs of successfully placed orders
	var wg sync.WaitGroup
	resultChan := make(chan struct{ OrderID string; Err error }, len(ordersToPlace)) // Channel to collect results

	for _, order := range ordersToPlace {
		if order == nil {
			c.logger.Warn("Skipping nil order in ordersToPlace list for Bybit")
			continue
		}
        // Note: We ignore order.Exchange here due to the constraint on processor.ProcessOrderbook
        // which always returns types.ExchangeInjective. This client assumes it's trading on Bybit.
        // if order.Exchange != types.ExchangeBybit {
        //     c.logger.Error("Skipping order not intended for Bybit in ordersToPlace list", "order", order)
        //     continue
        // }

		wg.Add(1)
		go func(ord types.Order) { // Pass order by value
			defer wg.Done()
			submitCtx, cancel := context.WithTimeout(ctx, 15*time.Second) // Timeout per submission
			defer cancel()

			// Use the SubmitOrder method to place each new order
			exchangeOrderID, submitErr := c.SubmitOrder(submitCtx, ord) // SubmitOrder handles ignoring the Exchange field

			resultChan <- struct{ OrderID string; Err error }{exchangeOrderID, submitErr}
		}(*order)
	}

	go func() {
		wg.Wait()
		close(resultChan) // Close when all submissions attempted
	}()

	// Collect results
	var firstError error
	failedCount := 0
	for res := range resultChan {
		if res.Err != nil {
			c.logger.Error("Failed to submit order during Bybit ReplaceQuotes batch", "error", res.Err)
			failedCount++
			if firstError == nil {
                firstError = res.Err
            }
		} else {
			c.logger.Info("Successfully submitted order during Bybit ReplaceQuotes batch", "exchange_order_id", res.OrderID)
			placedOrderIDs = append(placedOrderIDs, res.OrderID)
		}
	}

	if failedCount > 0 {
		return placedOrderIDs, types.TradingError{
			Code: types.ErrOrderSubmissionFailed,
			Message: fmt.Sprintf("Failed to place %d out of %d new orders during Bybit ReplaceQuotes batch", failedCount, len(ordersToPlace)),
			Wrapped: firstError,
		}
	}

	c.logger.Info("All new orders queued/sent for placement on Bybit during ReplaceQuotes.",
		"symbol", instrument.Symbol, "placed_count", len(placedOrderIDs))

	return placedOrderIDs, nil
}


// AmendOrder amends an existing order on Bybit by OrderId or ClientOrderId.
// This uses the Bybit REST API endpoint /v5/order/amend.
// This method is specific to the Bybit TradingClient and not part of the generic ExchangeClient interface.
func (c *TradingClient) AmendOrder(ctx context.Context, exchangeOrderID string, instrument *types.Instrument, price uint64, quantity uint64) (string, error) {
    if exchangeOrderID == "" {
        return "", fmt.Errorf("exchangeOrderID is empty")
    }
    if instrument == nil {
         return "", fmt.Errorf("instrument is nil")
    }
     if c.cfg.APIKey == "" || c.cfg.APISecret == "" || c.cfg.TradingURL == "" { // Use cfg fields
        return "", types.TradingError{
            Code: types.ErrConfigLoading,
            Message: "Bybit API trading config incomplete (key, secret, or url)",
        }
    }
     if price == 0 || quantity == 0 {
         return "", fmt.Errorf("price or quantity cannot be zero for amendment")
     }

    // Get market info for precise scaling
    info := c.getMarketInfo(instrument.Symbol)
     if info == nil {
         return "", types.TradingError{
             Code: types.ErrUnknown,
             Message: fmt.Sprintf("Market info not found for symbol %s, cannot amend order", instrument.Symbol),
         }
     }
      if info.Status != "Trading" {
          return "", types.TradingError{
             Code: types.ErrOrderRejected, // Or a more specific status code
             Message: fmt.Sprintf("Market %s is not in trading status (%s), cannot amend order", instrument.Symbol, info.Status),
         }
     }


	c.logger.Info("Attempting to amend order on Bybit REST API...",
        "order_id", exchangeOrderID,
        "symbol", instrument.Symbol,
        "new_price_scaled", price,
        "new_quantity_scaled", quantity,
    )

    // Convert scaled uint64 price/quantity to strings with correct precision using market info
    priceStr, priceErr := c.formatPrice(price, instrument.Symbol)
    if priceErr != nil {
        c.logger.Error("Failed to format price for Bybit amendment", "error", priceErr, "order_id", exchangeOrderID)
        return "", types.TradingError{
            Code: types.ErrOrderSubmissionFailed,
            Message: "Failed to format new price for API",
            Wrapped: priceErr,
        }
    }
     quantityStr, quantityErr := c.formatQuantity(quantity, instrument.Symbol)
    if quantityErr != nil {
         c.logger.Error("Failed to format quantity for Bybit amendment", "error", quantityErr, "order_id", exchangeOrderID)
         return "", types.TradingError{
            Code: types.ErrOrderSubmissionFailed,
            Message: "Failed to format new quantity for API",
            Wrapped: quantityErr,
        }
    }


	reqBody := AmendOrderRequest{ // Use the struct defined in messages.go
		Category: c.cfg.Category, // Use cfg.Category
		Symbol:   instrument.Symbol,
		OrderId:  exchangeOrderID, // Amend by exchange ID
		Qty:      quantityStr,
		Price:    priceStr,
	}

	// Execute the REST request using the helper
	endpoint := "/v5/order/amend"
    // No specific query params for amend POST body

	rawBody, err := c.sendBybitRequest(ctx, "POST", endpoint, nil, reqBody)

	if err != nil {
		c.logger.Error("Bybit amend order request failed", "error", err, "order_id", exchangeOrderID)
        // sendBybitRequest already wraps errors in TradingError
		return "", err // Return the wrapped error
	}

	// Parse the successful response body
	var apiResp AmendOrderResponse // Assumes AmendOrderResponse is defined in messages.go
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		c.logger.Error("Failed to unmarshal Bybit amend order response body", "error", err, "body", string(rawBody))
		return "", types.TradingError{ // FIXED: Return empty string along with the error
			Code: types.ErrUnknown,
			Message: "Failed to parse API response result for amend order",
			Wrapped: err,
		}
	}


    // Bybit amend can return the amended order ID or an error.
    // If successful, apiResp.Result.OrderId is the ID of the amended order.
    // The ID typically remains the same unless the order was somehow cancelled first.
    // We'll return the ID from the response as confirmation.

	c.logger.Info("Order amended successfully on Bybit",
        "order_id", apiResp.Result.OrderId,
        "client_order_id", apiResp.Result.ClientOrderId,
        "new_price_string", priceStr,
        "new_quantity_string", quantityStr,
    )

	// Return the exchange's order ID (should be the same as the input ID if successful)
	return apiResp.Result.OrderId, nil
}


// Close cleans up the Bybit trading client resources.
func (c *TradingClient) Close() error {
	c.cancel() // Cancel the client's context for graceful shutdown

	c.logger.Info("Bybit trading client shutting down...")

	// Closing the HTTP client is generally not strictly necessary as connections are short-lived
	// but can be done if needed.

	c.logger.Info("Bybit trading client shut down complete.")

	return nil
}

// Add this line at the end of the file to ensure it implements the interface
// This line must be outside any function or method.
var _ exchange.ExchangeClient = (*TradingClient)(nil)
