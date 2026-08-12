-- A bill is one closing cycle of one credit card: everything charged from the
-- day after the last closing through the closing date itself.
--
-- The row exists rather than being computed because a payment has to point at
-- something, and because total and status are recorded facts here: total is a
-- snapshot frozen when the cycle closes, and status is written as payments land.
CREATE TABLE card_bills (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  card_id    INTEGER NOT NULL REFERENCES credit_cards(id) ON DELETE CASCADE,
  closes_on  TEXT    NOT NULL CHECK (closes_on LIKE '____-__-__'),
  due_on     TEXT    NOT NULL CHECK (due_on    LIKE '____-__-__'),
  -- Minor units at the card's currency scale, like every other amount. Refreshed
  -- on every read while the bill is open; frozen once closes_on is in the past.
  total      INTEGER NOT NULL DEFAULT 0,
  status     TEXT    NOT NULL DEFAULT 'open'
             CHECK (status IN ('open', 'closed', 'partial', 'paid')),
  created_at TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT    NOT NULL DEFAULT (datetime('now')),
  -- A card closes once per closing date, so this is what lets bills be generated
  -- again on every read without ever writing a second copy.
  UNIQUE (card_id, closes_on)
);

-- Every bill list is per card, ordered by when it closed.
CREATE INDEX card_bills_card ON card_bills (card_id, closes_on);

-- A payment is an ordinary outcome on whichever account paid, which happens to
-- name a bill. Many rows may name one bill — that is what makes a partial
-- payment, and then another, work. Losing the bill unlinks them rather than
-- deleting money that really moved.
ALTER TABLE transactions ADD COLUMN pays_bill_id INTEGER REFERENCES card_bills(id) ON DELETE SET NULL;

-- An installment purchase is N real transactions, one per bill it lands on.
-- The group is the id of the first of them; seq and count are stored so
-- rendering "(3/5)" costs no extra query, and so the title stays what was typed.
ALTER TABLE transactions ADD COLUMN installment_group INTEGER;
ALTER TABLE transactions ADD COLUMN installment_seq   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transactions ADD COLUMN installment_count INTEGER NOT NULL DEFAULT 0;

CREATE INDEX transactions_installment_group ON transactions (installment_group);
CREATE INDEX transactions_pays_bill ON transactions (pays_bill_id);
