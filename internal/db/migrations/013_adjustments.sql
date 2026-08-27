-- The transactions table, rebuilt to admit a third kind: 'adjustment' — the
-- correction `kakei ac edit` files when a recorded balance is put right. No
-- form ever offers it, and it counts toward nothing: not income, not spending,
-- just the record catching up with reality.
--
-- Its value is signed — the one kind whose is. Everywhere else the value is
-- positive and the kind carries the sign; an adjustment is its own direction,
-- and inventing two kinds to say up and down would put the sign back into a
-- name again.
--
-- Rebuilt whole because SQLite cannot ALTER a CHECK. The runner turns
-- foreign_keys off around migrations, which is what keeps transaction_tags'
-- rows alive while the table under them is dropped and renamed — with the
-- pragma on, dropping the parent would cascade the tags away. Ids are copied
-- explicitly: installment_group, transfer_group and transaction_tags all point
-- at them.
CREATE TABLE transactions_new (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  title             TEXT    NOT NULL,
  description       TEXT    NOT NULL DEFAULT '',
  category_id       INTEGER REFERENCES categories(id) ON DELETE SET NULL,
  account_id        INTEGER REFERENCES accounts(id),
  card_id           INTEGER REFERENCES credit_cards(id),
  value             INTEGER NOT NULL CHECK (CASE WHEN kind = 'adjustment' THEN value <> 0 ELSE value > 0 END),
  kind              TEXT    NOT NULL CHECK (kind IN ('income', 'outcome', 'adjustment')),
  date              TEXT    NOT NULL CHECK (date LIKE '____-__-__'),
  created_at        TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at        TEXT    NOT NULL DEFAULT (datetime('now')),
  pays_bill_id      INTEGER REFERENCES card_bills(id) ON DELETE SET NULL,
  installment_group INTEGER,
  installment_seq   INTEGER NOT NULL DEFAULT 0,
  installment_count INTEGER NOT NULL DEFAULT 0,
  goal_id           INTEGER REFERENCES goals(id) ON DELETE SET NULL,
  recurring_id      INTEGER REFERENCES recurring_bills(id) ON DELETE SET NULL,
  cycle             TEXT    CHECK (cycle IS NULL OR cycle LIKE '____-__'),
  transfer_group    INTEGER,
  CHECK ((account_id IS NULL) <> (card_id IS NULL))
);

INSERT INTO transactions_new (id, title, description, category_id, account_id, card_id,
    value, kind, date, created_at, updated_at, pays_bill_id,
    installment_group, installment_seq, installment_count, goal_id, recurring_id, cycle, transfer_group)
  SELECT id, title, description, category_id, account_id, card_id,
    value, kind, date, created_at, updated_at, pays_bill_id,
    installment_group, installment_seq, installment_count, goal_id, recurring_id, cycle, transfer_group
  FROM transactions;

DROP TABLE transactions;
ALTER TABLE transactions_new RENAME TO transactions;

CREATE INDEX transactions_date              ON transactions (date);
CREATE INDEX transactions_installment_group ON transactions (installment_group);
CREATE INDEX transactions_pays_bill         ON transactions (pays_bill_id);
CREATE INDEX transactions_goal              ON transactions (goal_id);
CREATE INDEX transactions_recurring         ON transactions (recurring_id, cycle);
CREATE INDEX transactions_category_date     ON transactions (category_id, date);
CREATE INDEX transactions_transfer_group    ON transactions (transfer_group);
