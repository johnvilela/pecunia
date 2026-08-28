---
tags: [go, refactor, architecture, shared-kernel]
---

## Decision

Extracted a new `internal/core` package holding the currency/palette/code/money/picker helpers that both `internal/accounts` and the new `internal/cards` package need, rather than having `cards` import `accounts` directly or duplicating the logic.

## Why

Building the credit-card module needed roughly 90% of `account.go`'s non-domain surface (currencies, palette, code generation/validation, `ParseAmount`/`FormatAmount`) plus about half of `ui.go` (the picker, `Confirm`, dim styling). That much shared surface is a shared kernel, not a one-off import. Rejected alternatives:
- **`cards` imports `accounts` directly** — zero-line diff today, but hardwires a domain edge that reads wrong (`accounts.Currency` inside credit-card code, `accounts.ErrCancelled` returned by `pecunia cc`), and gets worse once a third consumer (transactions) arrives.
- **Duplicate the code** — rejected outright: it forks `ParseAmount`, a money-correctness path, into two copies that can drift.

## What moved to internal/core

`Currency`, `Currencies`, `CurrencyByCode`, `Color`, `Palette`, `ColorByName`, `CodeLen`, `codeAlphabet`, `RandomCode`, `NormalizeCode`, `ValidateCode`, `ParseAmount`, `FormatAmount`, `abs`, `isDigits`, `CodeErr` (the `UNIQUE`-constraint-to-readable-error helper), `ErrCancelled`, `DimColor`, `DimStyle`, `HeaderStyle`, `Confirm`, a generic `Pick[T any]`, and small `ColorOptions()`/`CurrencyOptions()` builders for `huh` select fields. Later in the same session, `core.ValidateName` was added too — see [[gotchas/huh-form-skips-validators-on-eof]].

**Generic picker, no interface noise:**
```go
type Choice struct{ Label, Desc, Filter string }
func Pick[T any](items []T, title string, row func(T) Choice) (T, error)
```
`accounts.Pick(accs []Account, title string) (Account, error)` kept its exact signature as a 3-line wrapper over `core.Pick`.

## What stayed in internal/accounts

`Account` (domain struct), `ErrNotFound` (kept the per-package "account not found" wording — every `errors.Is` call site was left untouched), `Store`, `Label`/`labelColor`/`balanceColor`/`styledAmount`/`frozenMark`, `Form`, `Table`, `Details`.

## Consequence

Two dead identifiers deleted during the move: `labelStyle` and `styledCode` in `internal/accounts/ui.go` — declared, never referenced by any live path (survivors of an earlier redesign); `go vet` doesn't flag unused package-level declarations, so they'd sat there undetected. Verified zero behavior change: all pre-existing subtests green, `gofmt`/`go vet` clean, before any credit-card code was written. Commit: `refactor: extract shared currency, palette and picker helpers into internal/core`.

Links: [[tasks/02-credit-card-module]] · [[decisions/0001-balance-as-int64-minor-units]] · [[decisions/0002-flat-cmd-package-layout]]