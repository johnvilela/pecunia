CREATE TABLE accounts (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  code        TEXT    NOT NULL UNIQUE CHECK (length(code) = 5),
  name        TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  color       TEXT    NOT NULL,
  balance     INTEGER NOT NULL DEFAULT 0, -- minor units; scale comes from currency (2 fiat, 8 BTC)
  currency    TEXT    NOT NULL,
  is_frozen   INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
