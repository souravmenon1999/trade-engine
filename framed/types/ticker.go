// types/ticker.go
package types

import "sync/atomic"

type Ticker struct {
    Timestamp atomic.Int64 // Unix timestamp
    Symbol    string       // e.g., BTCUSD
    Price     atomic.Int64 // Price in integer form (scaled)
    Quantity  atomic.Int64 // Quantity in integer form (scaled)
}