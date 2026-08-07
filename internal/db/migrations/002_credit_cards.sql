CREATE TABLE credit_cards (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  code         TEXT    NOT NULL UNIQUE CHECK (length(code) = 5),
  name         TEXT    NOT NULL,
  description  TEXT    NOT NULL DEFAULT '',
  color        TEXT    NOT NULL,
  credit_limit INTEGER NOT NULL DEFAULT 0, -- minor units; scale comes from currency. Named credit_limit because LIMIT is a keyword
  balance      INTEGER NOT NULL DEFAULT 0, -- minor units; what the open invoice already owes
  currency     TEXT    NOT NULL,
  closing_day  INTEGER NOT NULL CHECK (closing_day BETWEEN 1 AND 31), -- day of the month, every month
  due_day      INTEGER NOT NULL CHECK (due_day     BETWEEN 1 AND 31),
  created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);
