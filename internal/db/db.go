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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, err
	}
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
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
