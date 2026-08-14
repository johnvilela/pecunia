-- A recurring bill is the one that comes round every month: energy, Netflix,
-- rent. It is a template and nothing else — it holds no money and records no
-- payment. What has been paid is the transactions filed against it, worked out
-- on every read, the same call goals made: a stored copy is one more number
-- that can drift from the ledger, and the ledger is the record.
--
-- expected is what it usually costs, in the minor units of whatever it is paid
-- from. It is a starting point for the amount, not a fact: an energy bill is a
-- different number every month, which is the whole reason paying one asks.
-- Zero means nobody knows yet.
CREATE TABLE recurring_bills (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  code        TEXT    NOT NULL UNIQUE CHECK (length(code) = 5),
  name        TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  color       TEXT    NOT NULL DEFAULT 'blue',
  expected    INTEGER NOT NULL DEFAULT 0 CHECK (expected >= 0),
  -- What the payment gets filed under. A label, so losing it is not losing the
  -- bill — the same call the transactions table makes.
  category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL,
  -- Where the money comes from, and what currency the amounts are in: exactly
  -- one of an account or a credit card, like a transaction's own target. Left
  -- at NO ACTION, which refuses to delete an account that still pays a bill.
  account_id  INTEGER REFERENCES accounts(id),
  card_id     INTEGER REFERENCES credit_cards(id),
  -- The two days the whole module turns on: when the bill can be paid, and when
  -- it is late. Days a month is too short for are allowed, the way a card's
  -- closing day is — cards.NextDate clamps them. A due day *before* the open
  -- day is the normal shape of a bill that opens late in the month: it falls in
  -- the month after, which is what NextDate already does.
  open_day    INTEGER NOT NULL CHECK (open_day BETWEEN 1 AND 31),
  due_day     INTEGER NOT NULL CHECK (due_day  BETWEEN 1 AND 31),
  -- A cancelled subscription stops counting as due and keeps its history
  -- readable. Deleting it is still allowed and does something else entirely.
  active      INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  CHECK ((account_id IS NULL) <> (card_id IS NULL))
);

-- The tags every payment of this bill is filed with. Same shape as
-- transaction_tags, and the key is what keeps a tag from being listed twice.
CREATE TABLE recurring_bill_tags (
  bill_id INTEGER NOT NULL REFERENCES recurring_bills(id) ON DELETE CASCADE,
  tag     TEXT    NOT NULL,
  PRIMARY KEY (bill_id, tag)
);

-- A transaction may pay one recurring bill and never has to. Losing the bill
-- unlinks it rather than deleting money that really moved — a bill is a label
-- on the payment, not what the payment is.
ALTER TABLE transactions ADD COLUMN recurring_id INTEGER REFERENCES recurring_bills(id) ON DELETE SET NULL;

-- Which cycle the payment is *for*, as YYYY-MM, which is not the same as when
-- it was made: February's energy bill paid on 3 March clears February and
-- leaves March open. Without this column a late payment leaves the month it was
-- for overdue forever — and that is the month this module exists to catch.
--
-- NULL on every transaction that pays no bill; the CHECK is what keeps a full
-- date out of a column that means a month.
ALTER TABLE transactions ADD COLUMN cycle TEXT CHECK (cycle IS NULL OR cycle LIKE '____-__');

-- Every board reads the payments of one bill grouped by cycle, and every detail
-- view reads one bill's payments in date order.
CREATE INDEX transactions_recurring ON transactions (recurring_id, cycle);
