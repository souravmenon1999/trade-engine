package bybitApi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/shopspring/decimal"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl/bybit"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/types"
)

// Client represents a Bybit API client
type Client struct {
	apiKey     string
	apiSecret  string
	httpClient *http.Client
	baseURL    string
}

// NewClient initializes a new Bybit API client
func NewClient(apiKey, apiSecret string) *Client {
	return &Client{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    "https://api.bybit.com",
	}
}

// FetchHistoricalTrades fetches both closed trades and open positions to calculate realized and unrealized PnL
func (c *Client) FetchHistoricalTrades(ctx context.Context, category, symbol string) error {
	var allRecords []bybitTypes.ClosedPnLRecord
	cursor := ""

	// Step 1: Fetch closed PnL records for realized PnL
	for {
		queryParams := url.Values{}
		queryParams.Set("category", category)
		if symbol != "" {
			queryParams.Set("symbol", symbol)
		}
		queryParams.Set("limit", "100")
		if cursor != "" {
			queryParams.Set("cursor", cursor)
		}

		timestamp := time.Now().UnixNano() / int64(time.Millisecond)
		recvWindow := "5000"
		queryString := queryParams.Encode()
		payload := fmt.Sprintf("%d%s%s%s", timestamp, c.apiKey, recvWindow, queryString)
		signature := c.generateSignature(payload)

		url := fmt.Sprintf("%s/v5/position/closed-pnl?%s", c.baseURL, queryString)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("X-BAPI-API-KEY", c.apiKey)
		req.Header.Set("X-BAPI-TIMESTAMP", fmt.Sprintf("%d", timestamp))
		req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)
		req.Header.Set("X-BAPI-SIGN", signature)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to fetch closed PnL: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		var result bybitTypes.ClosedPnLResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		if result.RetCode != 0 {
			return fmt.Errorf("API error: code=%d, msg=%s", result.RetCode, result.RetMsg)
		}

		for _, record := range result.Result.List {
			bybitPnl.ProcessTrade(record, decimal.Zero, false) // Process closed trade
			allRecords = append(allRecords, record)
		}

		if result.Result.NextPageCursor == "" {
			break
		}
		cursor = result.Result.NextPageCursor
	}

	log.Printf("Fetched and processed %d Bybit closed trades.", len(allRecords))

	// Step 2: Fetch open positions for unrealized PnL
	openPositions, err := c.FetchOpenPositions(ctx, symbol)
	if err != nil {
		return fmt.Errorf("failed to fetch open positions: %w", err)
	}

	// Step 3: Fetch current market price for unrealized PnL calculation
	marketPrice, err := c.FetchMarketPrice(ctx, symbol)
	if err != nil {
		return fmt.Errorf("failed to fetch market price: %w", err)
	}

	// Step 4: Process open positions for unrealized PnL
	for _, pos := range openPositions {
		bybitPnl.ProcessTrade(bybitTypes.ClosedPnLRecord{}, marketPrice, true, pos)
	}

	log.Printf("Processed %d open positions for unrealized PnL.", len(openPositions))
	return nil
}

// FetchOpenPositions retrieves current open positions from Bybit
func (c *Client) FetchOpenPositions(ctx context.Context, symbol string) ([]bybitTypes.Position, error) {
	queryParams := url.Values{}
	queryParams.Set("category", "linear")
	queryParams.Set("symbol", symbol)

	timestamp := time.Now().UnixNano() / int64(time.Millisecond)
	recvWindow := "5000"
	queryString := queryParams.Encode()
	payload := fmt.Sprintf("%d%s%s%s", timestamp, c.apiKey, recvWindow, queryString)
	signature := c.generateSignature(payload)

	url := fmt.Sprintf("%s/v5/position/list?%s", c.baseURL, queryString)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-BAPI-API-KEY", c.apiKey)
	req.Header.Set("X-BAPI-TIMESTAMP", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)
	req.Header.Set("X-BAPI-SIGN", signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch open positions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result bybitTypes.PositionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.RetCode != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", result.RetCode, result.RetMsg)
	}

	return result.Result.List, nil
}

// FetchMarketPrice retrieves the current market price for a symbol
func (c *Client) FetchMarketPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	queryParams := url.Values{}
	queryParams.Set("category", "linear")
	queryParams.Set("symbol", symbol)

	url := fmt.Sprintf("%s/v5/market/tickers?%s", c.baseURL, queryParams.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to fetch market price: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result bybitTypes.TickerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return decimal.Zero, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.RetCode != 0 {
		return decimal.Zero, fmt.Errorf("API error: code=%d, msg=%s", result.RetCode, result.RetMsg)
	}

	if len(result.Result.List) == 0 {
		return decimal.Zero, fmt.Errorf("no ticker data found for symbol: %s", symbol)
	}

	price, err := decimal.NewFromString(result.Result.List[0].LastPrice)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid last price: %v", err)
	}

	return price, nil
}

// generateSignature creates an HMAC-SHA256 signature for authentication
func (c *Client) generateSignature(payload string) string {
	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}