---
name: pecunia-overview
description: Review the user's finances in pecunia and report where they stand, with alerts and tips grounded in their data. Use when the user asks for a financial overview, review or check-in, how their finances look, where their money stands, or what needs attention.
---

# pecunia-overview

Give the user an honest picture of where their money stands — what came in, what
went out, what is about to hurt, and what deserves attention. This skill is
read-only: make no writes at all.

## hard rules

- Amounts are integers in minor units — cents, or satoshis for BTC. R$ 100,00
  is `10000`, $5.99 is `599`, 0.1 BTC is `10000000`. Format amounts for
  display; never show the raw integers.
- Never sum or compare amounts across currencies — pecunia has no exchange
  rates. Report each currency on its own, always.
- Base every number on data pulled through the tools, never on memory or
  assumption.
- Respond in the user's language, whatever they wrote in.
- Every write through pecunia's tools is audited as source "ai" — the user can
  review it with pecunia_logs. This skill makes none.

## what to read

Pull, in this order:

1. `pecunia_summary` for the current month — totals in and out, balances, what
   is due and upcoming.
2. `pecunia_accounts` — every account's balance; note frozen ones.
3. `pecunia_credit_cards` and its bills — open statements, due dates, limit
   usage.
4. `pecunia_recurring_bills` — what is overdue, open, upcoming or already paid
   this cycle.
5. `pecunia_budgets` — each cap against what was actually spent.
6. `pecunia_goals` — progress toward each target.
7. `pecunia_transactions` for the current and previous months — only when you
   need to explain a number, not wholesale.

## what to report

Keep it scannable: a short verdict first, then the numbers.

- Balances per account, grouped by currency, with the total per currency.
- Money in vs money out this month, and how that compares to the previous
  month.
- What is due soon — recurring bills and card statements, with dates and
  amounts.
- Each budget: cap, spent, what is left, and how far through the month the
  user is.
- Each goal: target, progress, and whether the recent pace reaches it.

## alerts — lead with these when present

- Anything overdue: recurring bills past due, card statements unpaid past
  their due date.
- A budget at or over its cap — or on pace to blow through it before the
  month ends.
- A card at or over its limit, or close enough that the next statement lands
  it there.
- A negative or fast-shrinking account balance.
- A category spending clearly above its own recent months.

No alerts firing is worth saying too — one line, not a ceremony.

## tips

- Ground every tip in the data: name the category, the amount and the month
  it comes from. "Restaurants ran 40% over its three-month average" is a tip;
  "consider eating out less" is noise.
- Two or three tips at most, ranked by the money they touch.
- If the data is too thin to say anything useful, say that instead of
  inventing advice.
