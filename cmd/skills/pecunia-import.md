---
name: pecunia-import
description: Import transactions into pecunia from bank statements and exports the user provides — PDF, CSV, JSON or OFX — mapping them to accounts, cards and categories without creating duplicates. Use when the user shares a statement or export file, or asks to import, load or migrate their transactions.
---

# pecunia-import

Turn a statement file into clean ledger entries: parse it, map every row to
the right account or card and category, skip what is already filed, and only
then write — with the user's approval on the whole batch.

## hard rules

- Amounts are integers in minor units — cents, or satoshis for BTC. R$ 100,00
  is `10000`, $5.99 is `599`, 0.1 BTC is `10000000`. A statement that shows
  "100,00" must be filed as `10000`; getting this wrong corrupts the ledger a
  hundredfold.
- Never sum or compare amounts across currencies — pecunia has no exchange
  rates. Report totals per currency, always.
- Confirm with the user before every write — creates, updates, deletes. Reads
  need no confirmation.
- Respond in the user's language, whatever they wrote in.
- Every write is audited as source "ai" — the user can review it with
  pecunia_logs.

## parse

- Extract date, description and amount from whatever the user gave you — PDF,
  CSV, JSON, OFX. Ask for the column meaning or the sign convention only when
  the file leaves it genuinely ambiguous.
- The sign (or debit/credit column) decides income vs outcome. Card
  statements are usually all outcome with payments as the exception.
- Keep the original description — it is the user's best memory of what a row
  was.

## map

- Match the file to one account or card: `pecunia_accounts` and
  `pecunia_credit_cards` list what exists. When the statement does not
  obviously belong to one of them, ask — and offer to create it first.
- Categorize each row from the description using `pecunia_categories`. Leave a
  row uncategorized rather than guessing wildly; create a new category only
  when a real cluster of rows needs it, and only with approval.
- A movement between two of the user's own accounts is one transfer, not an
  income and an outcome — file it through the transfer action.
- A card purchase split in N parcels is one transaction with installments, not
  N separate entries.

## skip duplicates

Before writing anything, list `pecunia_transactions` over the file's date
range for the target account or card. A row whose date, amount and account
match an existing transaction is already filed — skip it and say so. When in
doubt (same amount, same day, twice on the statement), ask rather than guess.

## approve, then write

- Show the whole batch as one preview table — date, description, amount,
  category, account/card, and which rows will be skipped as duplicates — and
  wait for explicit approval.
- On approval, create the rows through `pecunia_transactions`, then report
  what landed: rows imported and skipped, and totals in and out per currency.
- If anything fails partway, report exactly which rows were written and which
  were not — never re-run the whole batch blind.
