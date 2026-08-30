---
name: pecunia-budget
description: Help the user create monthly budgets in pecunia from their real spending history. Use when the user asks to create a budget, set spending caps or limits per category, plan monthly spending, or asks how much they should spend on something.
---

# pecunia-budget

Propose monthly caps per category from what the user actually spends, agree on
them together, then file them. A budget invented without looking at the ledger
is a guess — read first.

## hard rules

- Amounts are integers in minor units — cents, or satoshis for BTC. R$ 100,00
  is `10000`, $5.99 is `599`, 0.1 BTC is `10000000`. Format amounts for
  display; never show the raw integers.
- Never sum or compare amounts across currencies — pecunia has no exchange
  rates. A budget lives in one currency; propose per currency, always.
- Read before you advise; confirm with the user before every write — creates,
  updates, deletes. Reads need no confirmation.
- Respond in the user's language, whatever they wrote in.
- Every write is audited as source "ai" — the user can review it with
  pecunia_logs.

## read first

1. `pecunia_transactions` for the last 2–3 full months, grouped by category —
   outcome only; ignore transfers and anything without a category.
2. `pecunia_categories` — the names and codes behind those groups.
3. `pecunia_budgets` — what already has a cap, active or archived.
4. `pecunia_summary` for a recent month, to sanity-check income vs the total
   you are about to propose.

## propose

- A cap per category that actually matters — skip categories with one or two
  stray transactions; a budget nobody can break is clutter.
- Base each cap on typical spend: trim obvious one-offs (a flight, a repair)
  before averaging, then add modest breathing room. Say what you trimmed and
  why.
- Check the sum of proposed caps per currency against that currency's income;
  caps that outrun income are a warning to raise, not hide.
- Categories that already have an active budget are not yours to move
  silently — show the current cap next to the proposed one and ask.

## approve, then write

- Show the full plan as one table: category, recent average, proposed cap,
  currency — and wait for explicit approval. Adjust and re-show if the user
  pushes back; never write a cap the user has not seen.
- On approval, file each budget through `pecunia_budgets` — create new ones,
  update the approved changes to existing ones. Report what was filed.

## follow up

- Point the user at their new caps: `pecunia_summary` (or `pecunia bg` in the
  CLI) shows spent vs cap through the month.
- Suggest revisiting after a full month of data — a first budget is a draft,
  not a verdict.
