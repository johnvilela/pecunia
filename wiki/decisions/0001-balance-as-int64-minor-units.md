---
tags: [money, schema, accounts, sqlite]
---

## Decision

The accounts `balance` column is a single SQLite `INTEGER` holding **minor units**, never a float. The scale comes from the account's currency: exponent 2 for USD/EUR/BRL, exponent 8 for BTC. `ParseAmount(s, currency)` / `FormatAmount(v, currency)` convert between the decimal string a user types/sees and the int64 minor-unit value, and `ParseAmount` **rejects** input with more decimal places than the currency's exponent allows rather than silently truncating it.

## Why

Came from an explicit correction during planning: the first accounts-module plan was rejected via ExitPlanMode with the feedback "Remenber that the balance column should be able to handle normal value and bitcoin value." The schema needed one column that holds both a $4200.00 fiat balance and a ₿1.50000000 Bitcoin balance without losing precision, and without a second column or type per currency.

## Details

- 4 currencies: Dollar, Euro, Brazilian Real (exponent 2), Bitcoin (exponent 8) — `internal/accounts/account.go`'s `Currency{Code, Label, Symbol, Exp}`.
- int64 headroom comfortably covers Bitcoin's 21M-coin cap even in satoshis.
- Verified live against a seeded DB: the BTC row stored as `150000000` (satoshis) and the BRL row as `123456` (centavos) in the same `balance` column, both round-tripping correctly through the CLI's table and details views.
- Tests cover round trips both directions for USD and BTC, rejection of excess-precision input (e.g. `1.234` for a 2-exponent currency), and int64 overflow.

Links: [[tasks/01-accounts-module]]