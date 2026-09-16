-- A note is a thought with a deadline and a priority: "get better health care",
-- "renegotiate the Itaú card". Its body is a markdown file in the notes
-- directory beside the database; this table keeps only what a list, a filter or
-- a score needs. The priority here is the one the owner wrote in the file. The
-- level pecunia shows is worked out on every read — from this base, the target,
-- how often the note is opened, how busy the accounts it names are, and how
-- long it has been left alone — and never stored, so it can never drift from
-- the file.
CREATE TABLE notes (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  title         TEXT    NOT NULL,
  priority      TEXT    NOT NULL DEFAULT 'medium'
                CHECK (priority IN ('low', 'medium', 'high', 'critical')),
  status        TEXT    NOT NULL DEFAULT 'open'
                CHECK (status IN ('open', 'doing', 'done', 'dropped')),
  -- A day, or nothing. What the owner typed ("in 3 months") is kept beside it
  -- so the file can say both.
  target        TEXT    NOT NULL DEFAULT '' CHECK (target = '' OR target LIKE '____-__-__'),
  target_phrase TEXT    NOT NULL DEFAULT '',
  -- The file's name inside the notes directory, never a full path: the
  -- directory can move (PECUNIA_NOTES) without a row going stale.
  path          TEXT    NOT NULL UNIQUE,
  -- The file as last indexed: its mtime in unix nanoseconds and a sha256 of
  -- the body. A different mtime is what makes a read re-parse the file; the
  -- hash is what says whether the body itself moved.
  mtime         INTEGER NOT NULL DEFAULT 0,
  body_hash     TEXT    NOT NULL DEFAULT '',
  read_count    INTEGER NOT NULL DEFAULT 0,
  edit_count    INTEGER NOT NULL DEFAULT 0,
  last_read_at  TEXT    NOT NULL DEFAULT '',
  created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- The default list is the open ones; --due and --overdue read the target.
CREATE INDEX notes_status ON notes (status);
CREATE INDEX notes_target ON notes (target);

-- Same shape as transaction_tags, and the same helpers normalise them.
CREATE TABLE note_tags (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag     TEXT    NOT NULL,
  PRIMARY KEY (note_id, tag)
);

CREATE INDEX note_tags_tag ON note_tags (tag);

-- What a note is about. No foreign key: ref_id points into one of three tables
-- depending on kind, and SQLite cannot say that. The store checks the
-- reference on every save; a goal deleted later simply stops resolving.
CREATE TABLE note_links (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  kind    TEXT    NOT NULL CHECK (kind IN ('account', 'card', 'goal')),
  ref_id  INTEGER NOT NULL,
  PRIMARY KEY (note_id, kind, ref_id)
);

CREATE INDEX note_links_ref ON note_links (kind, ref_id);
