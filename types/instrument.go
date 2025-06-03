package types

import (
	"fmt"      
	"sync/atomic" 
	"strconv"  
	
)


type Instrument struct {
    symbol            string
    baseCurrency      string // Assuming Currency is a string for simplicity
    quoteCurrency     string
    contractType      string // Using string to represent ContractType
    minQuantity       atomic.Int64
    tickSize          atomic.Int64
    minNotional       atomic.Int64
    maxLimitOrderQty  atomic.Int64
    maxMarketOrderQty atomic.Int64
    qtyStep           atomic.Int64
    multiplier        atomic.Int64
    maxLeverage       atomic.Int64
    active            atomic.Bool
    oraclePrice       atomic.Int64
    feeAdjustment     atomic.Int32 // Using Int32 as closest to i16 for atomic ops
    takerFeeNum       atomic.Int64
    takerFeeDenom     atomic.Int64
    makerFeeNum       atomic.Int64
    makerFeeDenom     atomic.Int64
    longRate          atomic.Int64
    shortRate         atomic.Int64
    fundingSet        atomic.Bool
    nextFundingTs     atomic.Int64
}

// NewInstrument creates a new Instrument instance
func NewInstrument(symbol, baseCurrency, quoteCurrency, contractType string) *Instrument {
    instr := &Instrument{
        symbol:        symbol,
        baseCurrency:  baseCurrency,
        quoteCurrency: quoteCurrency,
        contractType:  contractType,
    }
    instr.active.Store(true)
    instr.minQuantity.Store(UNSET_VALUE)
    instr.tickSize.Store(UNSET_VALUE)
    instr.minNotional.Store(UNSET_VALUE)
    instr.maxLimitOrderQty.Store(UNSET_VALUE)
    instr.maxMarketOrderQty.Store(UNSET_VALUE)
    instr.qtyStep.Store(UNSET_VALUE)
    instr.multiplier.Store(SCALE_FACTOR)
    instr.maxLeverage.Store(UNSET_VALUE)
    instr.oraclePrice.Store(UNSET_VALUE)
    instr.feeAdjustment.Store(int32(UNSET_VALUE))
    instr.takerFeeNum.Store(UNSET_VALUE)
    instr.takerFeeDenom.Store(UNSET_VALUE)
    instr.makerFeeNum.Store(UNSET_VALUE)
    instr.makerFeeDenom.Store(UNSET_VALUE)
    instr.longRate.Store(UNSET_VALUE)
    instr.shortRate.Store(UNSET_VALUE)
    instr.fundingSet.Store(false)
    instr.nextFundingTs.Store(UNSET_VALUE)
    return instr
}

// Perpetual creates a perpetual swap contract
func Perpetual(baseCurrency, quoteCurrency string) *Instrument {
    symbol := fmt.Sprintf("%s-PERP", baseCurrency)
    return NewInstrument(symbol, baseCurrency, quoteCurrency, "Perpetual")
}

// Helper functions to parse and scale values
func parseToInt64(value string) int64 {
    if v, err := strconv.ParseFloat(value, 64); err == nil {
        return int64(v * SCALE_FACTOR_F64)
    }
    return UNSET_VALUE
}

func parseToInt16(value string) int16 {
    if v, err := strconv.ParseFloat(value, 64); err == nil {
        return int16(v * float64(SCALE_FACTOR_I16))
    }
    return int16(UNSET_VALUE)
}

// isSet checks if a value is not the UNSET_VALUE
func isSet(value int64) bool {
    return value != UNSET_VALUE
}

// Update methods
func (i *Instrument) UpdateOrderConstraintsString(maxLimitQty, maxMarketQty, minQty, qtyStep, minNotional string) {
    i.maxLimitOrderQty.Store(parseToInt64(maxLimitQty))
    i.maxMarketOrderQty.Store(parseToInt64(maxMarketQty))
    i.minQuantity.Store(parseToInt64(minQty))
    i.qtyStep.Store(parseToInt64(qtyStep))
    i.minNotional.Store(parseToInt64(minNotional))
}

func (i *Instrument) UpdateTickSizeString(tickSize string) {
    i.tickSize.Store(parseToInt64(tickSize))
}

func (i *Instrument) UpdateTickSizeI64(tickSize int64) {
    i.tickSize.Store(tickSize)
}

func (i *Instrument) UpdateOraclePriceString(oraclePrice string) {
    i.oraclePrice.Store(parseToInt64(oraclePrice))
}

func (i *Instrument) UpdateFeeAdjustmentString(feeAdjustment string) {
    i.feeAdjustment.Store(int32(parseToInt16(feeAdjustment)))
}

func (i *Instrument) UpdateOrderConstraintsI64(maxLimitQty, maxMarketQty, minQty, qtyStep, minNotional int64) {
    i.maxLimitOrderQty.Store(maxLimitQty)
    i.maxMarketOrderQty.Store(maxMarketQty)
    i.minQuantity.Store(minQty)
    i.qtyStep.Store(qtyStep)
    i.minNotional.Store(minNotional)
}

func (i *Instrument) UpdateOraclePriceI64(oraclePrice int64) {
    i.oraclePrice.Store(oraclePrice)
}

func (i *Instrument) UpdateFeeAdjustmentI16(feeAdjustment int16) {
    i.feeAdjustment.Store(int32(feeAdjustment))
}

func (i *Instrument) UpdateFeeString(takerFee, makerFee string) {
    i.takerFeeNum.Store(parseToInt64(takerFee))
    i.takerFeeDenom.Store(SCALE_FACTOR)
    i.makerFeeNum.Store(parseToInt64(makerFee))
    i.makerFeeDenom.Store(SCALE_FACTOR)
}

func (i *Instrument) UpdateFeeI64(takerFeeNum, takerFeeDenom, makerFeeNum, makerFeeDenom int64) {
    i.takerFeeNum.Store(takerFeeNum)
    i.takerFeeDenom.Store(takerFeeDenom)
    i.makerFeeNum.Store(makerFeeNum)
    i.makerFeeDenom.Store(makerFeeDenom)
}

func (i *Instrument) UpdateNextFundingTsMs(nextFundingTsMs uint64) {
    current := i.nextFundingTs.Load()
    if current != int64(nextFundingTsMs) {
        i.nextFundingTs.Store(int64(nextFundingTsMs))
    }
}

func (i *Instrument) UpdateFundingRateF64(longRate, shortRate float64) {
    i.setFundingFlag()
    i.longRate.Store(int64(longRate * SCALE_FACTOR_F64))
    i.shortRate.Store(int64(shortRate * SCALE_FACTOR_F64))
}

// Getter methods
func (i *Instrument) GetNextFundingTsMs() uint64 {
    value := i.nextFundingTs.Load()
    if value == UNSET_VALUE {
        return 0
    }
    return uint64(value)
}

func (i *Instrument) setFundingFlag() {
    i.fundingSet.Store(true)
}

func (i *Instrument) IsFundingSet() bool {
    return i.fundingSet.Load()
}

func (i *Instrument) GetFundingRateF64() (float64, float64) {
    longRate := float64(i.longRate.Load()) / SCALE_FACTOR_F64
    shortRate := float64(i.shortRate.Load()) / SCALE_FACTOR_F64
    return longRate, shortRate
}

func (i *Instrument) GetOrderConstraints() (float64, float64, float64, float64, float64) {
    maxLimitQty := float64(i.maxLimitOrderQty.Load()) / SCALE_FACTOR_F64
    maxMarketQty := float64(i.maxMarketOrderQty.Load()) / SCALE_FACTOR_F64
    minQty := float64(i.minQuantity.Load()) / SCALE_FACTOR_F64
    qtyStep := float64(i.qtyStep.Load()) / SCALE_FACTOR_F64
    minNotional := float64(i.minNotional.Load()) / SCALE_FACTOR_F64
    return maxLimitQty, maxMarketQty, minQty, qtyStep, minNotional
}

func (i *Instrument) GetMinQtyI64() int64 {
    return i.minQuantity.Load()
}

func (i *Instrument) GetQtyStepI64() int64 {
    return i.qtyStep.Load()
}

func (i *Instrument) GetTickSize() float64 {
    return float64(i.tickSize.Load()) / SCALE_FACTOR_F64
}

func (i *Instrument) GetQtyStep() float64 {
    return float64(i.qtyStep.Load()) / SCALE_FACTOR_F64
}

func (i *Instrument) GetMakerFee() (float64, bool) {
    num := i.makerFeeNum.Load()
    denom := i.makerFeeDenom.Load()
    if isSet(num) && isSet(denom) {
        fee := float64(num) / float64(denom)
        if adj, ok := i.GetFeeAdjustment(); ok {
            fee += fee * adj
        }
        return fee, true
    }
    return 0, false
}

func (i *Instrument) GetTakerFee() (float64, bool) {
    num := i.takerFeeNum.Load()
    denom := i.takerFeeDenom.Load()
    if isSet(num) && isSet(denom) {
        fee := float64(num) / float64(denom)
        if adj, ok := i.GetFeeAdjustment(); ok {
            fee += fee * adj
        }
        return fee, true
    }
    return 0, false
}

func (i *Instrument) GetFeeAdjustment() (float64, bool) {
    value := i.feeAdjustment.Load()
    if value != int32(UNSET_VALUE) {
        return float64(value) / 100.0, true
    }
    return 0, false
}

func (i *Instrument) IsActive() bool {
    return i.active.Load()
}

func (i *Instrument) GetPrice() (float64, bool) {
    value := i.oraclePrice.Load()
    if isSet(value) {
        return float64(value) / SCALE_FACTOR_F64, true
    }
    return 0, false
}

func (i *Instrument) GetPriceI64() (int64, bool) {
    value := i.oraclePrice.Load()
    if isSet(value) {
        return value, true
    }
    return 0, false
}

func (i *Instrument) GetTickSizeU64() uint64 {
    return uint64(i.tickSize.Load())
}

func (i *Instrument) IsDerivative() bool {
    return i.contractType == "Futures" || i.contractType == "Perpetual"
}

// Equal compares two Instruments for equality based on key fields
func (i *Instrument) Equal(other *Instrument) bool {
    return i.symbol == other.symbol &&
        i.contractType == other.contractType &&
        i.baseCurrency == other.baseCurrency &&
        i.quoteCurrency == other.quoteCurrency
}