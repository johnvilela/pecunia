-- A budget is a monthly cap on what one category may cost. Goals say what is
-- being worked toward and recurring bills say what arrives whether you like it
-- or not; a budget is the third thing — what a discretionary category has been
-- decided to be worth.
--
-- It holds no money and records no spend. What a budget is at is the sum of the
-- transactions filed under its category in that month, worked out on every
-- read, the same call goals made: a stored copy is one more number that can
-- drift from the ledger, and the ledger is the record.
--
-- Nothing is added to the transactions table for this. A goal needs its own
-- column because linking one is a choice nobody can infer; a budget needs
-- nothing, because category_id, date and kind already say everything it wants
-- to know.
CREATE TABLE budgets (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  code        TEXT    NOT NULL UNIQUE CHECK (length(code) = 5),
  name        TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  color       TEXT    NOT NULL DEFAULT 'blue',
  -- Minor units at the budget's currency scale, like every other amount. A cap
  -- of zero is not a budget: a category costing nothing needs no cap.
  amount      INTEGER NOT NULL CHECK (amount > 0),
  -- The budget's own, like a goal's. Only transactions in it are counted, and
  -- there is no rate anywhere in pecunia to bring the others in.
  currency    TEXT    NOT NULL,
  -- What is capped. Unlike a recurring bill — which is still a bill without its
  -- category — a budget with no category is nothing at all, so this one goes
  -- when the category goes.
  category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  -- A budget that has been put away stops being tracked and keeps its history
  -- readable, the way an archived recurring bill does.
  active      INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  -- Two budgets over one category would each count the same spend, and neither
  -- figure would be wrong on its own — which is the worst kind of wrong. The
  -- currency is in the key because a category capped separately in reais and in
  -- satoshis is two real budgets over two disjoint sets of transactions.
  UNIQUE (category_id, currency)
);

-- Why a budget's amount is what it is, beside it rather than in it — the same
-- shape as goal_target_log, and for the same reason: a cap is a promise about
-- the future, and the future moves. Rice goes up and the food budget follows,
-- without losing the fact that it ever said R$800.00.
--
-- Each row carries what the amount was as well as what it became, so an entry
-- explains itself without walking the chain.
CREATE TABLE budget_amount_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  budget_id  INTEGER NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
  -- Both in minor units at the budget's currency scale, like every other amount.
  previous   INTEGER NOT NULL,
  amount     INTEGER NOT NULL CHECK (amount > 0),
  -- Why it moved. Optional: sometimes the number is the whole story.
  note       TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- An entry is worth nothing without the budget it describes — there is no money
-- in it to lose — so it goes when the budget goes.
CREATE INDEX budget_amount_log_budget ON budget_amount_log (budget_id, created_at);

-- No index on budgets(category_id): the UNIQUE above already is one.

-- What every budget's spend reads: one category over one month. transactions_date
-- covers the dates alone and the category filter would still scan them all.
CREATE INDEX transactions_category_date ON transactions (category_id, date);
