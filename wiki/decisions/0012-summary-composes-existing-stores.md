---
tags: [summary, cli, reporting, currency, clock, sqlite]
---

## Decision

`pecunia summary` adds no table, no migration and no SQL of its own. It calls the
stores every other command already calls, and renders with the table functions
those commands already render with. `internal/summary` holds two files: a
collector that gathers, and a renderer that prints.

## Why

Every figure a summary wants is already a query someone wrote: the ledger has
`Filter{From, To}`, recurring bills work their own status out from their
payments, goals sum from the ledger, statements generate on read. A summary that
wrote its own aggregates would be a second answer to questions the app already
answers — and the first time the two disagreed, the summary would be the one
that was wrong, because it is the one nobody edits from.

The cost of that choice is round trips, not correctness. A summary is roughly
ten to fifteen queries on a small database, plus one `Unpaid` per card. That is
the same walk `pecunia cc bill` already does over every card
([[decisions/0009-bills-as-rows-and-installments-as-transactions]]), against
local SQLite, once per command run.

## Details

**The clock is handed in, never asked for.** `Collect(conn, period, today)`, and
neither it nor `Render` calls `time.Now()`. Every status on the screen is judged
against one date, so the bills and the card statements can never be answering as
of different days. This is what forced `bills.NewStoreAt` into existence: the
statement store's clock was a field only its own package could reach.

**Two windows, and a bill lives in exactly one.**

| Section | Recurring bill | Card statement |
|---|---|---|
| DUE | `Current(today)` is `open` or `overdue` | `Unpaid`, `DueOn <= today` |
| NEXT 7 DAYS | an occurrence of this month or next, `upcoming`, opening in `(today, today+7]` | `Unpaid`, `DueOn` in `(today, today+7]` |

The seventh day counts in; today does not, because today already has its own
sections. Statements partition by construction. Bills can overlap — one overdue
for August and opening again on 1 September — so a bill already in DUE is never
added to the week ahead. Pay it now beats it opens again soon.

The week ahead cannot be read off `Current`, which stands at the oldest cycle
still unpaid and therefore says `paid` for a bill settled this month, hiding the
cycle opening in five days. It scans this month and next instead; seven days
cannot span more than one month boundary.

**Dates are compared as text.** `YYYY-MM-DD` sorts the way it reads, and a
string carries no timezone to disagree about — `monthRange` builds in UTC while
`today` is local, and comparing those as `time.Time` is a bug waiting.

**A window that is over is not a window with nothing in it.** When the period
does not contain today, what is due is not read and not reported. "nothing due"
under a month that ended in March is a claim about now that the screen never
checked.

**Money is `map[string]int64` keyed by currency, everywhere.** A single total
would be a bug waiting: bitcoin has eight decimal places and the fiat currencies
have two, and there is no rate in pecunia to convert between any of them. The net
is worked out over the union of the income and outcome keys, so a day that
earned only in bitcoin and spent only in reais prints both.

## Known holes

- **A read that writes.** Card statements are generated on read, so
  `pecunia summary` inserts missing `card_bills` rows and refreshes open totals.
  It is idempotent and it is the module's stated contract, but a summary takes a
  write lock, and no test may assert that nothing was created.
- **One `Unpaid` per card**, each of which is itself several queries. Fine for a
  handful of cards on local SQLite; the fix, if it is ever needed, is one query
  across all cards rather than a cache.
- **Card charges and the payment that settles them are both counted as out** —
  the charge on the day it happened, the bill payment on the day it cleared.
  Inherent to the ledger and already true of `pecunia t`, which is why the lines
  are labelled `in` and `out`, the ledger's own words, and never "spent".
- **Account balances and the out total do not reconcile**, because a card
  transaction moves no account balance. Correct, and deliberately not explained
  on screen with a reconciliation line.
- **`--month` prints every row**, however many that is. The user asked for the
  same sections over a wider window, so no cap was added.
- **No month-over-month comparison.** In day mode it needs a clamping rule no
  caption can state honestly (31 March against "31 February"); in month mode it
  would compare a partial month against a full one.

Links: [[tasks/08-summary-module]] ·
[[decisions/0011-recurring-bills-derived-from-payments]] ·
[[decisions/0010-goal-progress-summed-from-the-ledger]] ·
[[decisions/0009-bills-as-rows-and-installments-as-transactions]] ·
[[decisions/0002-flat-cmd-package-layout]] · [[rules/tdd]]
