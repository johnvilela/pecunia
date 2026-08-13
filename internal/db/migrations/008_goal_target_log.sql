-- Why a goal's target is what it is. A target is a promise about the future,
-- and the future moves: a R$5000.00 bill settles for R$3500.00 on an offer, and
-- the goal has to follow without losing the fact that it ever said R$5000.00.
--
-- The goal's own target column is still the live one; this is the history
-- beside it, one row per change. Each row carries what the target was as well
-- as what it became, so an entry explains itself without walking the chain —
-- and without needing the original target to have been logged at creation.
CREATE TABLE goal_target_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  goal_id    INTEGER NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  -- Both in minor units at the goal's currency scale, like every other amount.
  previous   INTEGER NOT NULL,
  target     INTEGER NOT NULL CHECK (target > 0),
  -- Why it moved. Optional: sometimes the number is the whole story.
  note       TEXT    NOT NULL DEFAULT '',
  -- When it moved, which is the point of keeping a log at all.
  created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Unlike a transaction, an entry here is worth nothing without the goal it
-- describes — there is no money in it to lose — so it goes when the goal goes.

-- Every read is one goal's history, oldest to newest or the other way round.
CREATE INDEX goal_target_log_goal ON goal_target_log (goal_id, created_at);
