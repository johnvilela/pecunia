// Package accounts holds the account domain: the model, its storage and its UI.
// Everything generic it needs — currencies, colors, codes, amounts — comes from
// kakei/internal/core.
package accounts

import (
	"errors"

	"kakei/internal/core"
)

type Account struct {
	ID          int64
	Code        string
	Name        string
	Description string
	Color       string
	Balance     int64 // minor units, scaled by Cur().Exp
	Currency    string
	IsFrozen    bool
	CreatedAt   string
	UpdatedAt   string
}

func (a Account) Cur() core.Currency { return core.CurrencyByCode(a.Currency) }
func (a Account) Col() core.Color    { return core.ColorByName(a.Color) }
func (a Account) Amount() string     { return core.FormatAmount(a.Balance, a.Cur()) }

var ErrNotFound = errors.New("account not found")
