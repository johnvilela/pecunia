-- Categories label a transaction. They carry no money and no currency: what a
-- category is worth is the sum of what is filed under it, which the
-- transactions module works out.
CREATE TABLE categories (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  code        TEXT    NOT NULL UNIQUE CHECK (length(code) = 5),
  name        TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  color       TEXT    NOT NULL,
  created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
