package types

import (
	"fmt"

	"github.com/shopspring/decimal" 
)

// Currency represents a currency or crypto asset.
// Fields remain exported.
type Currency struct {
	Code     string `json:"code"`
	Decimals uint32 `json:"decimals"`
}

// NewCurrency creates a new Currency instance and returns a pointer to it.
// This is common when you want to ensure a single instance is referenced.
func NewCurrency(code string, decimals uint32) *Currency {
	return &Currency{ // Note the '&' operator to get the memory address
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
// The receiver is now a pointer: '(c *Currency)'.
// Accessing fields: 'c.Decimals' still works, Go automatically dereferences.
func (c *Currency) Format(amount decimal.Decimal) string {
	if c == nil { // Pointers can be nil, so it's good practice to check
		return fmt.Sprintf("Invalid Currency (%s)", amount.String())
	}
	roundedAmount := amount.RoundDp(int32(c.Decimals))
	return fmt.Sprintf("%s %s", roundedAmount.String(), c.Code)
}

// String implements the fmt.Stringer interface for Currency.
// The receiver is now a pointer.
func (c *Currency) String() string {
	if c == nil {
		return "nil Currency"
	}
	return c.Code
}

// Amount represents a monetary amount with its associated currency.
// The Currency field is now a pointer to Currency.
type Amount struct {
	Value    decimal.Decimal `json:"value"`
	Currency *Currency       `json:"currency"` // Pointer to Currency
}

// NewAmount creates a new Amount instance and returns a pointer to it.
// It now takes a *Currency.
func NewAmount(value decimal.Decimal, currency *Currency) *Amount {
	return &Amount{
		Value:    value,
		Currency: currency, // currency is already a pointer
	}
}

// IsZero checks if the amount's value is zero.
// The receiver is now a pointer: '(a *Amount)'.
func (a *Amount) IsZero() bool {
	if a == nil {
		return true // A nil amount can be considered zero for this check
	}
	return a.Value.IsZero()
}

// Abs returns a new Amount instance (as a pointer) with the absolute value.
// It uses pointer receivers and returns a pointer.
func (a *Amount) Abs() *Amount {
	if a == nil {
		return NewAmount(decimal.Zero, nil) // Return a nil-currency zero amount if original is nil
	}
	return &Amount{
		Value:    a.Value.Abs(),
		Currency: a.Currency, // Currency is already a pointer, so we copy the pointer value
	}
}

// String implements the fmt.Stringer interface for Amount.
// The receiver is now a pointer.
// It handles potential nil Currency pointers.
func (a *Amount) String() string {
	if a == nil {
		return "nil Amount"
	}
	if a.Currency == nil {
		return fmt.Sprintf("%s <nil_currency>", a.Value.String())
	}
	return a.Currency.Format(a.Value)
}