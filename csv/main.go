package main

import (
    "encoding/base64"
    "encoding/csv"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "os"
    "strconv"
    "strings"

    "github.com/gorilla/websocket"
    "github.com/tendermint/tendermint/crypto/tmhash"
)

// TxEvent represents the structure of a transaction event from Injective's WebSocket
type TxEvent struct {
    Result struct {
        Data struct {
            Value struct {
                TxResult struct {
                    Height string `json:"height"`
                    Hash   string `json:"hash"` // Transaction hash, if provided by the event
                    Tx     string `json:"tx"`   // Base64-encoded transaction
                    Result struct {
                        GasUsed string `json:"gas_used"`
                        Events  []struct {
                            Type       string `json:"type"`
                            Attributes []struct {
                                Key   string `json:"key"`
                                Value string `json:"value"`
                            } `json:"attributes"`
                        } `json:"events"`
                    } `json:"result"`
                } `json:"TxResult"`
            } `json:"value"`
        } `json:"data"`
    } `json:"result"`
}

func main() {
    // Connect to Injective WebSocket
    url := "wss://sentry.tm.injective.network:443/websocket"
    c, _, err := websocket.DefaultDialer.Dial(url, nil)
    if err != nil {
        fmt.Println("Error connecting to WebSocket:", err)
        return
    }
    defer c.Close()

    // Subscribe to transaction events
    subMsg := map[string]interface{}{
        "jsonrpc": "2.0",
        "method":  "subscribe",
        "id":      1,
        "params": map[string]interface{}{
            "query": "tm.event='Tx'",
        },
    }
    if err := c.WriteJSON(subMsg); err != nil {
        fmt.Println("Error subscribing:", err)
        return
    }

    // Create CSV file
    f, err := os.Create("transactions.csv")
    if err != nil {
        fmt.Println("Error creating CSV file:", err)
        return
    }
    defer f.Close()

    writer := csv.NewWriter(f)
    defer writer.Flush()

    // Write CSV header
    if err := writer.Write([]string{"TxHash", "BlockHeight", "Fee", "GasUsed", "Methods"}); err != nil {
        fmt.Println("Error writing CSV header:", err)
        return
    }

    counter := 0
    const maxRows = 10000

    // Process WebSocket messages
    for {
        if counter >= maxRows {
            fmt.Println("Reached 10,000 transactions. Stopping.")
            break
        }

        _, message, err := c.ReadMessage()
        if err != nil {
            fmt.Println("Error reading message:", err)
            break
        }

        // Print raw message for debugging (uncomment to inspect event structure)
        fmt.Println("Raw message:", string(message))

        var event TxEvent
        if err := json.Unmarshal(message, &event); err != nil {
            fmt.Println("Error unmarshaling event:", err)
            continue
        }

        txResult := event.Result.Data.Value.TxResult

        // Extract block height
        blockHeight := txResult.Height
        if blockHeight == "" {
            blockHeight = "unknown"
            fmt.Println("Warning: Block height missing in event")
        }

        // Extract gas used
        gasUsed, err := strconv.ParseInt(txResult.Result.GasUsed, 10, 64)
        if err != nil {
            gasUsed = 0 // Default value if parsing fails
            fmt.Println("Error parsing gas used:", err)
        }

        // Extract fee from events
        var fee int64
        for _, e := range txResult.Result.Events {
            if e.Type == "tx" {
                for _, attr := range e.Attributes {
                    if attr.Key == "fee" {
                        feeStr := strings.ReplaceAll(attr.Value, " ", "")
                        coins := strings.Split(feeStr, ",")
                        for _, coin := range coins {
                            if strings.HasSuffix(coin, "inj") {
                                amountStr := strings.TrimSuffix(coin, "inj")
                                fee, err = strconv.ParseInt(amountStr, 10, 64)
                                if err != nil {
                                    fmt.Println("Error parsing fee:", err)
                                    fee = 0
                                }
                            }
                        }
                    }
                }
            }
        }

        // Extract methods from events
        methods := []string{}
        for _, e := range txResult.Result.Events {
            if e.Type == "message" {
                for _, attr := range e.Attributes {
                    if attr.Key == "action" {
                        if attr.Value != "" {
                            methods = append(methods, attr.Value)
                        }
                    }
                }
            }
        }
        methodsStr := strings.Join(methods, ",")
        if methodsStr == "" {
            methodsStr = "unknown"
            fmt.Println("Warning: No methods found in message events for tx:", txResult.Hash)
        }

        // Determine transaction hash
        txHash := ""
        if txResult.Hash != "" {
            txHash = txResult.Hash
        } else if txResult.Tx != "" {
            // Fallback to computing hash from tx field
            txBytes, err := base64.StdEncoding.DecodeString(txResult.Tx)
            if err != nil {
                fmt.Println("Error decoding tx:", err)
                txHash = "unknown"
            } else if len(txBytes) == 0 {
                fmt.Println("Warning: Decoded tx bytes are empty")
                txHash = "unknown"
            } else {
                txHash = strings.ToUpper(hex.EncodeToString(tmhash.Sum(txBytes)))
            }
        } else {
            fmt.Println("Warning: Both Hash and Tx fields are empty or missing")
            txHash = "unknown"
        }

        // Write row to CSV
        row := []string{
            txHash,
            blockHeight,
            strconv.FormatInt(fee, 10),
            strconv.FormatInt(gasUsed, 10),
            methodsStr,
        }
        if err := writer.Write(row); err != nil {
            fmt.Println("Error writing row:", err)
            continue
        }

        counter++
        if counter%100 == 0 {
            writer.Flush()
            fmt.Printf("Logged %d transactions\n", counter)
        }
    }

    // Final flush to ensure all data is written
    writer.Flush()
    fmt.Println("Processing complete. Total transactions logged:", counter)
}