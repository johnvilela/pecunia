---
tags: [recurring, bills, decisions, sqlite, schema]
---

# Recurring bills are templates; their state is derived from the payments

A recurring bill — energy, Netflix, rent — is a *template*: a code, what it
usually costs, where it is paid from, what it is filed under, and two days of
the month. It holds no money and records no payment.

Where a month stands is worked out on every read from the transactions filed
against it. This is [[decisions/0010-goal-progress-summed-from-the-ledger]]
again, for the same reason: a stored status is one more number that can drift
from the ledger, and the ledger is the record. There is no occurrence table, no
generation step and nothing to backfill.

## The four states

`upcoming → open → overdue → paid`, worked out from the calendar and the
payments in `Bill.Occurrence`. A fifth, `archived`, is a state of the bill
rather than of a cycle and only `Current` ever returns it — an archived bill
owes nothing, and months it never paid are not debts to chase.

The due date is `cards.NextDate(openOn, dueDay)`, which is why an energy bill
opening on the 28th and due on the 5th lands in the month after, and a day the
month is too short for lands on its last, without a case for either.

## The cycle is stored on the payment

`transactions.cycle` (`YYYY-MM`, NULL unless `recurring_id` is set) is the month
a payment was *for*, which is not always the month it was made in.

This is the column the module turns on. Without it, February's bill paid on
3 March marks March paid and leaves February overdue forever — and the month
somebody paid late is exactly the month a bill module exists to catch. With it,
`Current` returns the *oldest unpaid* cycle, so a paid August never hides an
unpaid July.

`Bill.cycles` floors the walk back at `LookBack` (12) months and at the month
the bill was created in: a bill owes nothing for the months before it existed,
and a bill unpaid for a year is not news a list can break.

## Two packages named for bills

`internal/bills` was already the credit-card statement
([[decisions/0009-bills-as-rows-and-installments-as-transactions]]), so this one
is `internal/recurring`. They are genuinely different things and a transaction
can carry both links at once: `pays_bill_id` settles a statement and moves the
card's balance, `recurring_id` is a label saying which monthly cost the money
went to. The CLI verb `kakei bill` was free — `kakei cc bill` is the card one —
and is what the user says out loud, so only the package had to move.

`internal/recurring` imports `internal/transactions`, unlike `goals` and
`bills`, which read the transactions table directly to dodge an import cycle.
Nothing in `transactions` imports `recurring`, so there is no cycle to dodge:
the bill reuses `transactions.Ref`, `NormalizeTags`, `MaxTags` and the whole
list-with-joins query instead of copying them.

## What this costs

- Every board is two queries: the bills, then one `GROUP BY recurring_id, cycle`
  for all their payments at once.
- A payment is an ordinary transaction. It moves the balance it came out of,
  and editing or deleting it later moves that balance back — the bill needs no
  say in it.
- Deleting a bill unlinks its payments (`ON DELETE SET NULL`) and clears their
  cycles in the same SQL transaction, since a month is only a month *of*
  something.

Links: [[tasks/07-recurring-bills-module]] ·
[[decisions/0010-goal-progress-summed-from-the-ledger]] ·
[[decisions/0009-bills-as-rows-and-installments-as-transactions]] ·
[[decisions/0008-transaction-double-entry-tags-and-filters]] · [[rules/tdd]]
