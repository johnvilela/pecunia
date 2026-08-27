---
tags: [sqlite, security, money, accounts, transactions]
---

## Decision

Four fixes, each its own commit, closing four of the five concrete defects an unprompted evaluation of kakei had named earlier in the session ([[sessions/c2b6cbbe-5735-4790-abac-4c4b5a60aca7]]). User: "Execute the small fixes, one commit per fix."

**WAL, a busy timeout, and one connection.** `db.Open` sets `journal_mode = WAL`, a `busy_timeout` long enough to be worth having (≥1000ms), and `SetMaxOpenConns(1)`. Without this a second process hit "database is locked" immediately — and `kakei bg`, `kakei s` and `kakei cc bill` all write on read paths ([[decisions/0009-bills-as-rows-and-installments-as-transactions]], [[decisions/0012-summary-composes-existing-stores]]), so it was worse than it looked. `TestOpenConcurrencySettings` pins all three settings, plus that reopening the same file keeps them — WAL is a property of the file, not the handle. Verified before committing: every `s.db` call site sits outside an open `inTx` transaction body (`bills.Refresh` takes the `tx` instead), so a single connection can't deadlock against a store holding its own transaction open. Commit `17a3e7a` — `fix(db): survive a second process with wal and a busy timeout`.

**File permissions.** The database file gets `0600`, and a directory kakei itself creates gets `0700` — but a directory that already existed (`KAKEI_DB` may point into `$HOME` or anywhere else) is left exactly as it was, since tightening permissions on a directory kakei didn't make isn't its call. The `-wal` and `-shm` sidecar files get `0600` too, since the `-wal` holds the most recent writes. Commit `bf9980b` — `fix(db): keep the database readable only by its owner`.

**Currency frozen once anything is filed.** `accounts.Store.Update` and `cards.Store.Update` both now refuse a currency change once any transaction is recorded against the account or card, via a new `Store.Linked(id)` that counts them. Moving a currency under live history converts nothing — the balance, the limit, and every recorded amount stay the same integers, silently re-read at a different scale (BRL centavos read as BTC satoshis). Same call [[decisions/0010-goal-progress-summed-from-the-ledger]] and the budgets module ([[tasks/09-budgets-module]]) already made for goals and budgets — this closes the two modules that were still missing it. Commit `b7c3c6e` — `fix(accounts): freeze the currency once money is recorded against it`.

**Frozen accounts refuse new money.** `transactions.Store.Create` and `Update` both call a new `refuseFrozen` before writing. Placed in the **store**, not in `Transaction.Validate` alongside the other rules — a `frozen bool` field on the struct would default to `false` (meaning "allowed"), so a caller that only knew the account id would sail straight through a validation-only guard. Only the write side is checked: money already on a frozen account can still be moved off it or the transaction deleted, or freezing would be a way to lose track of money rather than close an account down. A bill payment (`PayBill`) is refused the same way, since it goes through `Create`. Commit `ec98f13` — `fix(transactions): keep new money off a frozen account`.

## Verification

TDD throughout, full suite green, `gofmt`/`go vet` clean after each commit, and the dev database was reseeded and re-checked live (`kakei bg`, `kakei s`) after all four landed, including confirming the file is `-rw-------` on disk.

## Known gaps — from the same evaluation, still open

- **Balance is a hand-editable cached aggregate.** `kakei ac edit` still accepts any balance directly, with no adjustment transaction, no log, and no `reconcile` command to detect drift against the ledger — the one number in kakei that can silently disagree with the transactions supposed to explain it (contrast [[decisions/0010-goal-progress-summed-from-the-ledger]] and [[decisions/0011-recurring-bills-derived-from-payments]], which compute rather than store for exactly this reason). Requested as the next piece of work ("Lets do the balance reconcile fix") right as this session ended — not started. **Resolved since**: balance edits now file signed adjustment transactions and card balances are ledger-only — [[decisions/0015-balance-adjustments-as-a-hidden-kind]].
- **Read paths still write.** `bills.Ensure` still generates card statements on read ([[decisions/0009-bills-as-rows-and-installments-as-transactions]]), so `kakei s` mutates the database. WAL makes this survivable under concurrent access rather than fixing it. **Resolved since**: reads write only when a closing date has passed; charges refresh their own bill inside the write tx — [[decisions/0016-bill-reads-stop-writing]].
- **No non-interactive write path and no machine-readable output.** Every create/edit is a `huh` form, and there is zero `encoding/json` in the repo — both named as the hard prerequisite for the planned chatbot/AI integration, and neither exists yet.
- **`wiki/index.md` doesn't exist**, though `AGENTS.md` tells every agent to read it first.

Links: [[sessions/c2b6cbbe-5735-4790-abac-4c4b5a60aca7]] · [[tasks/09-budgets-module]] · [[tasks/10-transfers]] · [[decisions/0010-goal-progress-summed-from-the-ledger]] · [[decisions/0009-bills-as-rows-and-installments-as-transactions]] · [[rules/tdd]]