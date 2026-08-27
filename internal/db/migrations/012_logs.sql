-- One row per logical action: something was created, edited or deleted. A
-- transfer, an installment purchase or a bill payment is one action however
-- many rows it wrote, and the side-effects inside one — balances moved, bill
-- totals refreshed, tags rewritten — are the action's mechanics, not actions
-- of their own.
--
-- goal_target_log and budget_amount_log stay what they are: value history with
-- meaning of its own. This table answers a different question — what was done,
-- to what, by whom, when.
CREATE TABLE logs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  -- Who caused it: the user at the terminal, kakei itself (generated card
  -- bills, the starter categories), or — reserved, nothing writes it yet — an
  -- AI on the other end of an integration.
  source     TEXT    NOT NULL CHECK (source IN ('user', 'system', 'ai')),
  action     TEXT    NOT NULL CHECK (action IN ('created', 'edited', 'deleted')),
  -- One of: account, card, category, transaction, transfer, goal, recurring,
  -- budget, card_bill. No CHECK on purpose: the next module should not need a
  -- migration to be allowed to log.
  entity     TEXT    NOT NULL,
  -- No foreign key, also on purpose: the trail outlives what it describes, or
  -- deleting a thing would erase the record of ever having had it.
  entity_id  INTEGER NOT NULL,
  -- On an edit, the fields that changed and nothing else, as JSON keyed by
  -- field: {"name":{"old":"Cash","new":"Wallet"}}. Empty on created and
  -- deleted — there the row itself is the whole story.
  changes    TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- --entity and --id narrow on the first; the plain listing and the date range
-- read the second.
CREATE INDEX logs_entity  ON logs (entity, entity_id);
CREATE INDEX logs_created ON logs (created_at);
