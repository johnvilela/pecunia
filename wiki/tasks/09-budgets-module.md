---
tags: [budgets, module, tui, sqlite, tdd, planning]
---

# Budgets module

A budget is a monthly cap on what one category may cost. Goals say what you are
working toward; recurring bills say what arrives whether you like it or not. A
budget is the third thing: what you have decided a discretionary category is
allowed to be. It holds no money and records no spend — what a budget is at is
the sum of the transactions filed under its category in that month, worked out
on every read, the same call [[tasks/06-goals-module]] made.

User: "Lets plan the budgets. They will be similar to the goals. Ask me anything
to refine it." Four design questions went back before any code; the answers are
the decisions below.

## Status: Implemented

Built TDD in the `budgets-module` session, on the plan below, which was written
first and followed. Four design questions went to the user before any code; the
answers are the decisions in it. What the build found that the plan did not:

- **`GROUP BY cycle` is a trap in this schema.** `transactions` has a column
  literally called `cycle` — the month a recurring bill's payment is for
  ([[decisions/0011-recurring-bills-derived-from-payments]]) — so the history
  query's `GROUP BY cycle` bound to that column instead of to the `substr` alias
  beside it, and every month collapsed into one NULL group. Caught by the
  history test on the first run. It groups by the expression now.
- **Deleting a category quietly took its budgets with it.** The cascade is
  right — a budget with no category is nothing — but `kakei ct delete` said only
  "This cannot be undone." The confirmation counts them now, through
  `budgets.Store.CountForCategory`. Introduced by this migration, so fixed in
  it.
- **The pace tick inside the bar was dropped.** It would have cost the fixed
  width the table column lines up on, and the drift line says the same thing in
  money and more precisely. Noted as a `ponytail:` comment on `bar`.
- **An archived budget still reports "R$150.00 over" in its LEFT column**, and
  should: that is a fact about the month's spend against the cap. What it must
  not carry is the verdict — no "on track", no "ahead", no pace line on the
  card. The first draft of the table test conflated the two.

Verified live against the seeded dev database: `kakei bg`, `bg CODE`,
`--month`, `--all`, `archive`/`unarchive`, and the summary section. Six budget
fixtures were added to `scripts/seed`, one archived and one whose cap has moved,
so both branches have something to render.

Not built, as planned: rollover, weekly or yearly budgets, a total across
budgets, and any warning at the transaction form.

## Commands

```
kakei budget                     this month's budgets and where they stand
kakei bg                         the alias
kakei budget new       | bg n    create one
kakei budget edit  {CODE|ID?}    edit, asking why if the amount moved
kakei budget delete {CODE|ID?}   delete for good
kakei budget archive {CODE|ID?}  stop tracking, keep the history
kakei budget {CODE|ID?}          details: this month, the pace, the amount log
kakei budget --month YYYY-MM     any month
kakei budget --all               archived ones too
```

Every `{CODE|ID?}` opens a picker when left out, `-h` on any command prints that
command's own help, both the way every other module already does it.

## Decisions

- **Nothing is added to the `transactions` table.** This is the one place
  budgets diverge from goals. A goal needs `goal_id` because linking one is a
  choice nobody can infer; a budget needs nothing, because `category_id`, `date`
  and `kind` already say everything a budget wants to know. File a transaction
  under Food and it counts against the Food budget with no second decision, no
  extra form field, and no way to forget. The transaction form does not change
  at all.

- **One category per budget**, asked and confirmed. `UNIQUE (category_id,
  currency)` is what keeps two budgets from claiming the same spend — without it
  a transaction counts twice and neither figure is wrong on its own, which is
  the worst kind of wrong. Two budgets on one category in *different* currencies
  is still allowed, and is the only reason the currency is in the key.

- **Grouping is a category, not a feature.** "Living" spanning rent, energy and
  water was offered and turned down. If it is wanted later it is a category
  named Living, not a join table.

- **One amount, and a log of every time it moved**, verbatim the shape of
  `goal_target_log` ([[decisions/0010-goal-progress-summed-from-the-ledger]]).
  The budget's own `amount` is the live one; `budget_amount_log` is the history
  beside it, each row carrying `previous` as well as `amount` so an entry
  explains itself without walking the chain, and a `note` for why. Raising the
  food budget from R$800 to R$950 because rice went up is a fact worth keeping.

  The cost, accepted knowingly: looking at July after raising the amount in
  August shows July judged against the new number. The log is where the old one
  lives. Goals already work exactly this way, and a per-month overrides table
  was offered and turned down as the second concept it would cost.

- **A card purchase counts on the day it was charged**, not the day the
  statement is settled. Asked and confirmed. R$200 of food on the card on 15
  August is August's food, even though the money leaves in September — a budget
  is about what you consumed, and every other date in kakei is the transaction's
  own. It also keeps a month's food from mixing this month's cash with last
  month's card.

- **Installments need no special case.** A purchase split over six bills is
  already six rows on six dates ([[decisions/0009-bills-as-rows-and-installments-as-transactions]]),
  so each month's budget sees its own sixth. That falls out of the date rule
  above for free, and it is the right answer: the R$200 is what that month
  actually costs you.

- **A budget never refuses a transaction.** Asked and confirmed. A credit
  card's limit is a fact about the world and `ValidateBalance` is right to
  enforce it; a budget is a promise to yourself, and a ledger that refuses to
  record real purchases is a ledger you stop trusting — or work around by
  leaving the transaction uncategorised, which loses the spend *and* the
  category. Over budget is shown, loudly, and recorded.

- **A budget counts one currency**, the same call goals make and for the same
  reason: centavos and satoshis do not add up and there is no rate anywhere in
  kakei to make them. Unlike a goal, nothing is linked to a budget, so the
  currency cannot be checked at write time — it is filtered on read, through
  the account or card the transaction names.

- **Refunds net off.** Income filed under Food lowers Food's spend, using the
  same `SUM(CASE WHEN kind ...)` goals uses. A R$40 refund on a R$200 grocery
  run should give you R$40 of budget back, because it gave you R$40 back.

- **A budget has a code**, unlike a goal and like a category. Goals are
  referenced by id because a goal is a one-off you look at occasionally; a
  budget is a standing name you type all month, next to the category it caps —
  which already has one.

- **Archive, not just delete.** `active`, like a recurring bill: a budget you
  have stopped tracking stops appearing and keeps its history readable.
  Deleting is still allowed and takes only the budget — the transactions were
  never linked to it in the first place, so there is nothing to unlink.

## Schema

One migration, no change to any existing table.

```sql
-- 010_budgets.sql
CREATE TABLE budgets (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  code        TEXT    NOT NULL UNIQUE CHECK (length(code) = 5),
  name        TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  color       TEXT    NOT NULL DEFAULT 'blue',
  -- Minor units at the budget's currency scale, like every other amount. A
  -- budget of zero is not a budget; a category you spend nothing on needs no cap.
  amount      INTEGER NOT NULL CHECK (amount > 0),
  currency    TEXT    NOT NULL,
  -- What is capped. A budget with no category is nothing at all -- unlike a
  -- recurring bill, which is still a bill -- so this one goes when the category
  -- goes, and it is the only NOT NULL reference in the schema that cascades.
  category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  active      INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  -- Two budgets on one category would each count the same spend, and neither
  -- figure would be wrong on its own. The currency is in the key because a
  -- category capped separately in BRL and in BTC is two real budgets.
  UNIQUE (category_id, currency)
);

-- Why a budget's amount is what it is, beside it rather than in it. Same shape
-- as goal_target_log, and for the same reason: a cap is a promise about the
-- future, and the future moves.
CREATE TABLE budget_amount_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  budget_id  INTEGER NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
  previous   INTEGER NOT NULL,
  amount     INTEGER NOT NULL CHECK (amount > 0),
  note       TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX budget_amount_log_budget ON budget_amount_log (budget_id, created_at);
```

No index on `budgets(category_id)` — the UNIQUE already is one.

The spend query needs `transactions (category_id, date)`, and the existing
`transactions_date` index does not cover it. Add it in the same migration:

```sql
CREATE INDEX transactions_category_date ON transactions (category_id, date);
```

## Store

Spend rides back with the row it belongs to, as a correlated subquery in
`columns` — the same trick `goals/store.go` uses, and for the same reason: one
plain SELECT per method, and a `Get` for a missing id comes back as no rows
rather than one all-NULL row.

The wrinkle goals does not have is the currency. A transaction carries no
currency column — it inherits one from whatever it is filed against — so the
subquery has to reach the account or the card to filter:

```sql
COALESCE((
  SELECT SUM(CASE WHEN t.kind = 'outcome' THEN t.value ELSE -t.value END)
  FROM transactions t
  LEFT JOIN accounts     a ON a.id = t.account_id
  LEFT JOIN credit_cards k ON k.id = t.card_id
  WHERE t.category_id = b.category_id
    AND COALESCE(a.currency, k.currency) = b.currency
    AND t.date >= ? AND t.date <= ?          -- the cycle's first and last day
), 0)
```

Dates are compared as text with the month's bounds, not `strftime`, so the new
index is usable — everywhere else in kakei already compares `YYYY-MM-DD` as
text, and the month bounds are what `monthRange` in `cmd/transactions.go`
already builds.

```go
NewStore(db) *Store

List(cycle string, archived bool) ([]Budget, error)  // spend for that cycle
Get(id int64) (Budget, error)                        // cycle set by the caller
ByCode(code string) (Budget, error)
Resolve(ref string) (Budget, error)                  // id when digits, else code
Create(b *Budget) error
Update(b Budget, note string) error                  // logs when amount moved
SetActive(id int64, active bool) error
Delete(id int64) error
AmountLog(id int64) ([]AmountChange, error)
CodeTaken(code string) (bool, error)
SuggestCode() (string, error)
History(id int64, months int) ([]Cycle, error)       // spend per month, for details
```

`Update` refuses a currency change while any transaction in the old currency
falls under the category — the guard `goals/store.go:98` already implements,
lifted. It also refuses a category change into one another budget already caps
in that currency, which is the UNIQUE spoken as a sentence rather than as a
constraint error.

## Model

`internal/budgets/budget.go`, mirroring `Goal`. Everything here is a pure
function over a struct literal, so the whole thing is testable without a
database — which is what made `goals` and `recurring` cheap to test, and is the
point of keeping the arithmetic out of the SQL.

```go
type Budget struct {
    ID, Amount int64
    Code, Name, Description, Color, Currency string
    Category transactions.Ref
    Active   bool
    // Cycle is the month this reading is for, and Spent is what the store
    // summed for it. Never columns: both are set on every read.
    Cycle string  // YYYY-MM
    Spent int64
    CreatedAt, UpdatedAt string
}

func (b Budget) Remaining() int64    // Amount - Spent; goes negative, nothing clamps
func (b Budget) Over() bool
func (b Budget) Fmt(v int64) string
func (b Budget) Validate() error     // store boundary guard, as everywhere
```

The part worth building, and the reason a budget beats a number at month end:

```go
// Pace is what should have been spent by today if the month were spent evenly.
// Integer arithmetic, like every other amount -- no float touches money.
func (b Budget) Pace(today time.Time) int64   // Amount * elapsed / daysInMonth
func (b Budget) Drift(today time.Time) int64  // Spent - Pace; positive is ahead
func (b Budget) Status(today time.Time) string
```

`elapsed` is clamped to `[0, daysInMonth]`, which is what makes a past month
read at full pace and a future one at none, with no case for either. A month
that is over is judged against the whole amount; a month that has not started
has nothing to be ahead of.

```go
const (
    StatusOnTrack  = "on track" // at or under the pace
    StatusAhead    = "ahead"    // past the pace, still inside the amount
    StatusOver     = "over"     // past the amount
    StatusArchived = "archived" // a state of the budget, not of the month
)
```

Four states, a function of the calendar and the ledger, so nothing is stored and
nothing can drift — the same shape as `recurring`'s five.

## UI

`internal/budgets/ui.go`: `Table`, `Pick`, `Form`, `Details`, all the shapes the
other modules already ship. The list is one row per budget with a bar:

```
BUDGET      SPENT              LEFT       PACE
Food        R$540.00 / 800.00  R$260.00   ████████░░  ahead R$24.00
Transport   R$120.00 / 300.00  R$180.00   ████░░░░░░  on track
Leisure     R$470.00 / 400.00  -R$70.00   ██████████  over R$70.00
```

Over is red, ahead is amber, on track is green — the states already in `core`'s
palette, and the same colour language the card and goal views use. The bar is
lipgloss and nothing else; no new dependency.

`cmd/budgets.go` mirrors `cmd/goals.go` line for line, including the `-h` per
subcommand and the `{CODE|ID}` picker fallback.

## Summary

One section in `internal/summary`, which composes stores and writes no SQL of
its own ([[decisions/0012-summary-composes-existing-stores]]) — so this is one
`budgets.NewStore(conn).List(cycle, false)` and one renderer, with the same
`today` every other status on the screen is judged against.

On a month summary it is the month's budgets. On a day summary it is the same
budgets read for the month that day falls in, beside the month-to-date figure
that is already there — which is the pairing that answers "can I afford this
today", and is the reason to build the module at all.

## Out of scope

- **Rollover.** Unspent R$100 in August does not become R$900 in September.
  Envelope budgeting wants it; it needs a whole second concept of a balance
  carried between months, and every month becomes a function of every month
  before it. Not asked for, not built.
- **Weekly or yearly budgets.** Monthly only. The cycle is already the app's
  unit — recurring bills, card statements and `--month` all speak it.
- **A total across all budgets.** It would have to add currencies, and nothing
  in kakei does that.
- **Budgeting uncategorised spend.** It has no category, so no budget catches
  it. Worth a line in the summary if it turns out to be large, but that is a
  separate observation and not a budget.
- **Warning at the transaction form.** Rejected with the refuse option: it only
  ever reaches the interactive path, so a future bot or import would not see it,
  and a rule that holds in one path and not the others is not a rule.

## Tests

The pattern the other modules established, TDD as [[rules/tdd]] has it:

- `budget_test.go` — `Pace`, `Drift`, `Status`, `Remaining` as table tests over
  struct literals, no database. Month-length edges are the ones that bite:
  February, a 31-day month, day 0, and a cycle entirely in the past or future.
- `schema_test.go` — the CHECKs actually fire: `amount > 0`, the
  `UNIQUE (category_id, currency)`, and the cascade from a deleted category.
- `store_test.go` — spend nets refunds, ignores other currencies, ignores other
  months, counts a card charge on its charge date, counts one installment and
  not the series, and the amount log records only real moves.
- `ui_test.go` — the bar and the three states render, over included.
- `cmd/budgets_test.go` — the command surface, the aliases, the `-h` texts.
