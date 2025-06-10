package injective

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// GasPriceHandler handles gas price updates from Injective WebSocket
type GasPriceHandler struct {
	callback func(int64)
}

// NewGasPriceHandler creates a new GasPriceHandler instance
func NewGasPriceHandler(callback func(int64)) *GasPriceHandler {
	return &GasPriceHandler{callback: callback}
}

// Handle processes transaction messages to extract and update gas prices
func (h *GasPriceHandler) Handle(message []byte) error {
    // log.Debug().Str("rawMessage", string(message)).Msg("Received WebSocket message")
    var msg map[string]interface{}
    if err := json.Unmarshal(message, &msg); err != nil {
        log.Error().Err(err).Msg("Failed to unmarshal message")
        return err
    }

    // Handle subscription response
    if success, ok := msg["success"].(bool); ok && success {
        log.Info().Msg("Subscription to transaction events confirmed")
        return nil
    }

    // Handle transaction event
    result, ok := msg["result"].(map[string]interface{})
    if !ok {
        log.Warn().Msg("No result field in message")
        return fmt.Errorf("no result field")
    }
    data, ok := result["data"].(map[string]interface{})
    if !ok {
        log.Warn().Msg("No data field in result")
        return fmt.Errorf("no data field")
    }
    value, ok := data["value"].(map[string]interface{})
    if !ok {
        log.Warn().Msg("No value field in data")
        return fmt.Errorf("no value field")
    }
    txResult, ok := value["TxResult"].(map[string]interface{})
    if !ok {
        log.Warn().Msg("No TxResult field in value")
        return fmt.Errorf("no TxResult field")
    }
    resultInner, ok := txResult["result"].(map[string]interface{})
    if !ok {
        log.Warn().Msg("No result field in TxResult")
        return fmt.Errorf("no result field in TxResult")
    }

    gasUsedStr, ok := resultInner["gas_used"].(string)
    if !ok {
        log.Warn().Msg("No gas_used field in result")
        return fmt.Errorf("no gas_used field")
    }
    gasUsed, err := strconv.ParseInt(gasUsedStr, 10, 64)
    if err != nil || gasUsed == 0 {
        log.Error().Err(err).Msg("Invalid gas_used value")
        return err
    }

    var totalFee int64
    events, ok := resultInner["events"].([]interface{})
    if ok {
        for _, e := range events {
            event, ok := e.(map[string]interface{})
            if !ok || event["type"] != "tx" {
                continue
            }
            attributes, ok := event["attributes"].([]interface{})
            if !ok {
                continue
            }
            for _, attr := range attributes {
                attrMap, ok := attr.(map[string]interface{})
                if !ok || attrMap["key"] != "fee" {
                    continue
                }
                feeStr := strings.ReplaceAll(attrMap["value"].(string), " ", "")
                coins := strings.Split(feeStr, ",")
                for _, coin := range coins {
                    if strings.HasSuffix(coin, "inj") {
                        amountStr := strings.TrimSuffix(coin, "inj")
                        fee, err := strconv.ParseInt(amountStr, 10, 64)
                        if err == nil {
                            totalFee += fee
                        }
                    }
                }
            }
        }
    }

    if totalFee == 0 {
        log.Warn().Msg("No fee found in transaction events")
        return fmt.Errorf("no fee found")
    }

    txGasPrice := totalFee / gasUsed
    if h.callback != nil {
        h.callback(txGasPrice)
    }

    currentTime := time.Now().Format("3:04PM MST")
    log.Info().Int64("gasPrice", txGasPrice).Str("time", currentTime).Msg("Gas price update processed successfully")
    return nil
}