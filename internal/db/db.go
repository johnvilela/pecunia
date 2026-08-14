// Package db opens the kakei database and keeps its schema up to date.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DevDB is empty in a release build and set to the seeded database at the repo
// root by scripts/dev.sh, via
// -ldflags "-X kakei/internal/db.DevDB=<path>".
var DevDB string

// Path returns the database file. A dev build always uses DevDB and nothing
// else — not even $KAKEI_DB — so a dev binary can never reach the real
// database. A release build uses $KAKEI_DB when set, otherwise
// <user config dir>/kakei/kakei.db. UserConfigDir honours XDG_CONFIG_HOME and
// resolves to ~/.config on Linux.
func Path() (string, error) {
	if DevDB != "" {
		return DevDB, nil
	}
	if p := os.Getenv("KAKEI_DB"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kakei", "kakei.db"), nil
}

// Open opens the database, creating the file and its directory if needed, and
// applies any migrations it is missing.
func Open() (*sql.DB, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	// 0700 applies only to directories this creates. One that already exists is
	// left alone: KAKEI_DB may point into $HOME, or /tmp, or anywhere else, and
	// tightening a directory kakei did not make is not kakei's call.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection, so two writes inside a process queue instead of colliding.
	// Nothing here holds a transaction open while querying the pool — every
	// store either passes its *sql.Tx down or does its reads before Begin — so
	// this cannot deadlock.
	conn.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		// Write-ahead logging, so a reader never blocks the writer and the
		// writer never blocks a reader. This is a property of the file rather
		// than of the handle: the second opener finds it already set.
		"PRAGMA journal_mode = WAL",
		// And when two writers do meet — a chatbot beside a terminal — wait for
		// the lock rather than failing the command outright. Without it SQLite
		// returns "database is locked" the instant it cannot get in.
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	// Before the migrations, so SQLite takes the mode from an already-shut file
	// when it makes the -wal and -shm beside it.
	if err := shut(path); err != nil {
		conn.Close()
		return nil, err
	}
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, shut(path)
}

// shut takes the owner's read and write off everything else. Every transaction
// ever recorded is in this one file, and the -wal beside it holds the most
// recent of them — on a shared machine the default umask leaves both readable
// by anyone with an account.
//
// A file that is not there yet has nothing to leak: the -wal and -shm appear
// only once something is written, which is why this runs on both sides of the
// migrations rather than once.
func shut(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// migrate applies every embedded migration not yet recorded in
// schema_migrations, each in its own transaction. Running it twice is a no-op.
func migrate(conn *sql.DB) error {
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}

	rows, err := conn.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// ReadDir returns entries sorted by filename, which is why they are numbered.
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if applied[e.Name()] {
			continue
		}
		stmts, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := conn.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(stmts)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", e.Name()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
