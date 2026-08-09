-- A transaction is money moving. It carries no currency of its own: value is in
-- the minor units of whatever it is filed against, which is exactly one of an
-- account or a credit card.
--
-- value is always positive and kind carries the sign, so nothing has to agree on
-- which direction a negative number meant.
CREATE TABLE transactions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  title       TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  -- A category is a label, so losing one is not losing the transaction.
  category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL,
  -- A target is not a label. These two are left at the default NO ACTION, which
  -- is what refuses to delete an account or card that still has money filed
  -- against it.
  account_id  INTEGER REFERENCES accounts(id),
  card_id     INTEGER REFERENCES credit_cards(id),
  value       INTEGER NOT NULL CHECK (value > 0),
  kind        TEXT    NOT NULL CHECK (kind IN ('income', 'outcome')),
  date        TEXT    NOT NULL CHECK (date LIKE '____-__-__'),
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  -- Exactly one target: never both, never neither.
  CHECK ((account_id IS NULL) <> (card_id IS NULL))
);

-- Every list either filters by date or orders by it.
CREATE INDEX transactions_date ON transactions (date);

CREATE TABLE transaction_tags (
  transaction_id INTEGER NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
  tag            TEXT    NOT NULL,
  -- The key is also what keeps a tag from being listed twice on one row.
  PRIMARY KEY (transaction_id, tag)
);

-- The tag filter and the autocomplete's list of tags already in use both read
-- this column and nothing else.
CREATE INDEX transaction_tags_tag ON transaction_tags (tag);
