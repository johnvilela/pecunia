-- A goal is something being worked toward: money set aside for something, or a
-- debt worked down. It holds no money and records no progress — what a goal is
-- at is the sum of the transactions filed against it, worked out on every read.
-- A stored copy would be one more number that can drift from the ledger, and
-- the ledger is the record.
--
-- The currency is the goal's own: target is in its minor units, and only a
-- transaction in the same currency may be linked, because cents and satoshis do
-- not add up. kind is what the sum means — a saving goal climbs on money in, a
-- paying one on money out, so both read as progress toward a positive target.
CREATE TABLE goals (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  -- Minor units at the goal's currency scale, like every other amount.
  target      INTEGER NOT NULL CHECK (target > 0),
  currency    TEXT    NOT NULL,
  kind        TEXT    NOT NULL CHECK (kind IN ('saving', 'paying')),
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- A transaction may name one goal and never has to. Losing the goal unlinks
-- them rather than deleting money that really moved — the same call category_id
-- makes, and for the same reason: a goal is a label on the transaction, not
-- what the transaction is.
ALTER TABLE transactions ADD COLUMN goal_id INTEGER REFERENCES goals(id) ON DELETE SET NULL;

-- Every goal's progress is this lookup, and listing the goals runs it once per
-- goal — without the index that is a scan of the whole ledger each time.
CREATE INDEX transactions_goal ON transactions (goal_id);
