package bybitTypes

// ClosedPnLRecord defines a single closed PnL record from Bybit
type ClosedPnLRecord struct {
	Symbol        string `json:"symbol"`
	OrderID       string `json:"orderId"`
	Side          string `json:"side"`
	Qty           string `json:"qty"`
	OrderPrice    string `json:"orderPrice"`
	OrderType     string `json:"orderType"`
	ExecType      string `json:"execType"`
	ClosedSize    string `json:"closedSize"`
	CumEntryValue string `json:"cumEntryValue"`
	AvgEntryPrice string `json:"avgEntryPrice"`
	CumExitValue  string `json:"cumExitValue"`
	AvgExitPrice  string `json:"avgExitPrice"`
	ClosedPnl     string `json:"closedPnl"`
	FillCount     string `json:"fillCount"`
	Leverage      string `json:"leverage"`
	CreatedTime   string `json:"createdTime"`
	UpdatedTime   string `json:"updatedTime"`
}

// ClosedPnLResponse defines the structure of the API response for closed PnL
type ClosedPnLResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		NextPageCursor string            `json:"nextPageCursor"`
		Category       string            `json:"category"`
		List           []ClosedPnLRecord `json:"list"`
	} `json:"result"`
	Time int64 `json:"time"`
}

// Position defines a single open position from Bybit
type Position struct {
    Symbol        string `json:"symbol"`
    PositionIdx   int    `json:"positionIdx"` // Changed to int
    Side          string `json:"side"`
    Size          string `json:"size"`
    EntryPrice    string `json:"entryPrice"`
    LiqPrice      string `json:"liqPrice"`
    BustPrice     string `json:"bustPrice"`
    Leverage      string `json:"leverage"`
    PositionValue string `json:"positionValue"`
    UnrealisedPnl string `json:"unrealisedPnl"`
}

// PositionResponse defines the structure of the API response for open positions
type PositionResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []Position `json:"list"`
	} `json:"result"`
	Time int64 `json:"time"`
}

// Ticker defines a single ticker from Bybit's market data
type Ticker struct {
	Symbol     string `json:"symbol"`
	LastPrice  string `json:"lastPrice"`
}

// TickerResponse defines the structure of the API response for market tickers
type TickerResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []Ticker `json:"list"`
	} `json:"result"`
	Time int64 `json:"time"`
}