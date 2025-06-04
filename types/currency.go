package types

import (
	"fmt"

	"github.com/shopspring/decimal" 
)

// Currency represents a currency or crypto asset.
type Currency struct {
	Code     string `json:"code"`
	Decimals uint32 `json:"decimals"`
}

// NewCurrency creates a new Currency instance and returns a pointer to it.
func NewCurrency(code string, decimals uint32) *Currency {
	return &Currency{ 
		Code:     code,
		Decimals: decimals,
	}
}

// Fiat creates a fiat currency and returns a pointer to it.
func Fiat(code string) *Currency {
	return NewCurrency(code, 2)
}

// Crypto creates a crypto currency and returns a pointer to it.
func Crypto(code string) *Currency {
	return NewCurrency(code, 8)
}

// Format takes a decimal amount and formats it using the Currency pointer.
func (c *Currency) Format(amount decimal.Decimal) string {
	if c == nil { 
		return fmt.Sprintf("Invalid Currency (%s)", amount.String())
	}
	roundedAmount := amount.RoundDp(int32(c.Decimals))
	return fmt.Sprintf("%s %s", roundedAmount.String(), c.Code)
}

// String implements the fmt.Stringer interface for Currency.

func (c *Currency) String() string {
	if c == nil {
		return "nil Currency"
	}
	return c.Code
}

// Amount represents a monetary amount with its associated currency.

type Amount struct {
	Value    decimal.Decimal `json:"value"`
	Currency *Currency       `json:"currency"`
}

// NewAmount creates a new Amount instance and returns a pointer to it.

func NewAmount(value decimal.Decimal, currency *Currency) *Amount {
	return &Amount{
		Value:    value,
		Currency: currency, // currency is already a pointer
	}
}

// IsZero checks if the amount's value is zero.

func (a *Amount) IsZero() bool {
	if a == nil {
		return true // A nil amount can be considered zero for this check
	}
	return a.Value.IsZero()
}

// Abs returns a new Amount instance (as a pointer) with the absolute value.

func (a *Amount) Abs() *Amount {
	if a == nil {
		return NewAmount(decimal.Zero, nil)
	}
	return &Amount{
		Value:    a.Value.Abs(),
		Currency: a.Currency, 
	}
}


func (a *Amount) String() string {
	if a == nil {
		return "nil Amount"
	}
	if a.Currency == nil {
		return fmt.Sprintf("%s <nil_currency>", a.Value.String())
	}
	return a.Currency.Format(a.Value)
}