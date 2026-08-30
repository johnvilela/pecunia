---
name: pecunia-health
description: Hunt money leaks in the user's pecunia ledger and propose concrete fixes to improve their financial health. Use when the user asks where their money is going, how to save or cut expenses, about money leaks, forgotten subscriptions, or their overall financial health.
---

# pecunia-health

Find where money quietly leaks — the forgotten subscription, the category that
creeps a little every month, the fees nobody questioned — quantify each leak,
and propose fixes worth their trouble.

## hard rules

- Amounts are integers in minor units — cents, or satoshis for BTC. R$ 100,00
  is `10000`, $5.99 is `599`, 0.1 BTC is `10000000`. Format amounts for
  display; never show the raw integers.
- Never sum or compare amounts across currencies — pecunia has no exchange
  rates. Quantify each leak in its own currency, always.
- Read before you advise; confirm with the user before every write — creates,
  updates, deletes. Reads need no confirmation.
- Respond in the user's language, whatever they wrote in.
- Every write is audited as source "ai" — the user can review it with
  pecunia_logs.

## read

1. `pecunia_transactions` for the last 3 months or more — the longer the
   window, the more patterns hold up.
2. `pecunia_recurring_bills` — what the user already tracks as recurring.
3. `pecunia_credit_cards` and its bills — statements, limit usage, and
   anything that looks like interest or fees.
4. `pecunia_budgets` — where caps exist and how they held.

Less than a month of data supports almost none of the checks below — say so
and stick to what it can support.

## hunt leaks

- Same amount to the same name, month after month, that is not registered as
  a recurring bill — a subscription flying under the radar.
- A recurring charge the ledger shows no use for lately — the gym from
  January, the streaming service nobody watches. Flag it; the user knows
  which ones are alive.
- A category growing month over month while nothing else changed.
- Many small charges from one merchant or category that compound into a real
  monthly number — delivery apps are the classic.
- Interest, late fees and service charges — on card bills and in the ledger.
  These are pure leak; no lifestyle argument defends them.
- Overdue recurring bills and card statements — lateness is tomorrow's fee.

## quantify and rank

For every leak, put a monthly number on it in its own currency, and show the
evidence — the transactions or months behind it. Rank by impact; three leaks
worth real money beat ten worth pocket change. Skip anything too small to act
on.

## propose fixes

Each fix names its leak and what it saves per month:

- Cancel or downgrade what is unused — the user confirms which.
- Cap a creeping category with a budget through `pecunia_budgets`.
- Register an untracked subscription as a recurring bill through
  `pecunia_recurring_bills`, so it shows up on the board instead of hiding.
- Kill fee-generators: pay the overdue thing, set the reminder, move the due
  date discussion with the bank.
- Turn a saving into a target through `pecunia_goals` when the user wants to
  see it accumulate.

Writes only with explicit approval, one decision at a time — this skill's job
is a shorter list of leaks next month, not a pile of unreviewed changes.
