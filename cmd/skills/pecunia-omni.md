---
name: pecunia-omni
description: How pecunia thinks and what its Omni Telegram commands do. Use when pecunia runs as an Omni plugin, when deciding between the MCP tools and the /pecunia chat commands, or when the user asks what pecunia can do from chat.
---

# pecunia-omni

Pecunia is the user's ledger, living in one SQLite file on their machine. When
it runs as an Omni plugin you reach it two ways: the MCP tools
(`pecunia_summary`, `pecunia_transactions`, ...) for anything that needs
reading, judgement or writing — and the `/pecunia` Telegram commands, which
run instantly with no model involved. Know both, and send each job to the
cheaper one that does it.

## hard rules

- Amounts are integers in minor units — cents, or satoshis for BTC. R$ 100,00
  is `10000`, $5.99 is `599`, 0.1 BTC is `10000000`. Format amounts for
  display; never show the raw integers.
- Never sum or compare amounts across currencies — pecunia has no exchange
  rates. Report per currency, always.
- Read before you advise; confirm with the user before every write — creates,
  updates, deletes. Reads need no confirmation.
- Respond in the user's language, whatever they wrote in.
- Every write is audited as source "ai" — the user can review it with
  pecunia_logs.

## how pecunia thinks

- Everything is local: one SQLite file, no cloud, no sync, no upsell.
- Derived figures — goal progress, budget spend, a bill's status — are never
  stored. They are recomputed from the ledger on every read, so nothing can
  drift from the record.
- Entities are addressed by 5-character codes (accounts, cards, categories,
  bills, budgets); goals go by id.

## the Telegram commands

These print stored data as plain text, instantly and at zero model cost. When
the user asks for something one of them already answers, point them at it
instead of re-computing it yourself:

- `/pecunia-resume [period]` — balances, money in and out, alerts. Periods:
  today, yesterday, week, last week, month, last month, YYYY-MM, YYYY-MM-DD.
- `/pecunia-goals` — every goal and its progress.
- `/pecunia-bills` — recurring bills and where this cycle stands.
- `/pecunia-cc` — cards: limit, used, available, the open statement.
- `/pecunia-budget` — this month's caps against actual spend.
- `/pecunia-alerts` — only problems; silent when all is well. Suggest it to
  the user as an Omni scheduled task for a daily nudge that costs nothing.
- `/pecunia-add AMOUNT TITLE [@ACCOUNT] [#CATEGORY]` — quick expense, e.g.
  `/pecunia-add 12.50 lunch #food`. With one account the @CODE is optional;
  writes it makes are the user's own, not source "ai".

## what stays yours

Anything beyond echoing stored data is MCP-tool work: comparing months,
explaining a number, importing statements, proposing budgets, hunting money
leaks. For those, follow the pecunia-overview, pecunia-budget, pecunia-import
and pecunia-health skills.
