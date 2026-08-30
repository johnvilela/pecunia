package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"pecunia/internal/categories"
	"pecunia/internal/core"
	"pecunia/internal/db"
)

func runSetup(force, skills bool) error {
	path, err := db.Path()
	if err != nil {
		return err
	}

	existed := false
	switch _, err := os.Stat(path); {
	case err == nil && !force:
		// Not a no-op any more: setup still runs so a database that predates a
		// module gets its starter rows, which beats making the user reach for
		// --force and back up real data to catch up.
		fmt.Printf("database already exists at %s — use --force to back it up and create a new one\n", path)
		existed = true
	case err == nil:
		// Rename, not copy: atomic, and the original is never left half-written.
		backup := fmt.Sprintf("%s.%s.bak", path, time.Now().UTC().Format("20060102T150405Z"))
		if err := os.Rename(path, backup); err != nil {
			return err
		}
		fmt.Println("backed up existing database to", backup)
	case !os.IsNotExist(err):
		return err
	}

	conn, err := db.Open()
	if err != nil {
		return err
	}
	defer conn.Close()
	if !existed {
		fmt.Println("created database at", path)
	}

	n, err := categories.Seed(categories.NewStore(conn))
	if err != nil {
		return err
	}
	if n > 0 {
		fmt.Printf("seeded %d categories\n", n)
	}
	// The explicit flag installs without prompting, the way an agent argument
	// does for "pecunia mcp install".
	if skills {
		return installSkills()
	}
	// A declined or TTY-less prompt (a script, a test) is a no either way,
	// which is why the error is dropped. "pecunia mcp install" re-offers it.
	if ok, _ := core.Confirm("Hook pecunia up to an AI agent?",
		"Registers this binary's MCP server. Re-run anytime: pecunia mcp install",
		"Yes, install"); ok {
		// Backing out of the agent picker shouldn't take the skills offer
		// down with it.
		if err := runMCPInstall(nil); err != nil && !errors.Is(err, core.ErrCancelled) {
			return err
		}
	}
	if ok, _ := core.Confirm("Teach your agent to manage your money?",
		"Installs pecunia's finance skills for claude-code, codex, gemini and opencode. Re-run anytime: pecunia setup --skills",
		"Yes, install"); ok {
		return installSkills()
	}
	return nil
}
