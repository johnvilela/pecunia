---
tags: [accounts, validation, bug]
---

## What happened

User hit a real bug: typed their own account code, "INTER", during creation, and validation rejected it. Root cause: `ValidateCode` reused `RandomCode`'s reduced alphabet (excludes visually ambiguous `O`/`0`, `I`/`1`), which was meant only for *generated suggestions*, not for codes the user types themselves.

## Fix

`internal/accounts/account.go`: `ValidateCode` was `strings.ContainsRune(codeAlphabet, r)`, now checks against the full `A-Z0-9`. `RandomCode` still uses the reduced alphabet for what it generates — only the validator changed. One place fixed, so the `huh` form validator in `ui.go` and both the `Create`/`Update` paths follow automatically. The error message no longer dumps the full alphabet; it now reads "code may only use letters and digits". Still rejected: wrong length, punctuation, inner space, accented letters — matches the DB's `CHECK (length(code) = 5)`.

## Consequence flagged, left unsolved

"INTER" and "1NTER" can now both exist as distinct codes. The ambiguity between visually similar codes was explicitly left as the user's problem to look at, not the generator's.

## Why it happened

Generation-time constraints (avoid ambiguous characters in *suggestions*) and validation-time constraints (what's *allowed* when typed) are different concerns. Sharing one alphabet constant between `RandomCode` and `ValidateCode` silently blocked valid user input.

Links: [[tasks/01-accounts-module]] · [[decisions/0004-dev-build-isolated-by-ldflags]]