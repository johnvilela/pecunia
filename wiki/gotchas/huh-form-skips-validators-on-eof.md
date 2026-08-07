---
tags: [tui, huh, validation, bug]
---

## What happened

User asked "Does the seed on dev db updated with cards?" while verifying, a card turned up in the dev DB with no name — `[PXQ55]`. Root cause: under a pty whose stdin hits EOF, `huh`'s `form.Run()` returns `nil` without ever running the field validators, including the "name is required" check. The create command then happily inserted a row with a blank name. This affected both `kakei cc n` and `kakei ac n` — pre-existing in `accounts` from the original module, just first surfaced now, not introduced by the credit-card work.

## Fix

`core.ValidateName(s string) error` added to `internal/core` — the one shared definition of "a name is required". Called as a guard inside `Store.Create` and `Store.Update` in **both** `internal/cards/store.go` and `internal/accounts/store.go`, ahead of the insert/update — the boundary every caller crosses (the interactive form, the seed script, and any future caller), not just the form. Both `huh` forms (`cards.Form`, `accounts.Form`) were also switched from their own inline `strings.TrimSpace(v) == ""` validator closures to `.Validate(core.ValidateName)`, so there's exactly one definition instead of two that could drift.

## Verification

Reproduced with a pty whose stdin is closed immediately (`printf '\033' | script -qec "... cc n" /dev/null`, and the `ac n` equivalent) before the fix — both inserted a blank-named row. Re-ran the same repro after the fix — both now insert nothing. The junk `PXQ55` row was deleted from the dev DB by hand. New store-level tests (`TestNameIsRequired` in both `cards` and `accounts` store test files) pin the guard directly, without needing a TTY.

## Why it happened

Same shape as [[gotchas/account-code-validation-vs-generation-alphabet]]: validating only inside the TUI form left the actual data boundary — the store — unguarded. Form-time and store-time validation are different trust boundaries, and only one of them is guaranteed to run.

Links: [[gotchas/account-code-validation-vs-generation-alphabet]] · [[decisions/0005-internal-core-shared-kernel]] · [[tasks/02-credit-card-module]] · [[sessions/ce07d7cb-4a82-4381-89cb-9ad513a7159d]]