package main

import (
	"fmt"
	"os"
	"time"

	"kakei/internal/db"
)

func runSetup(force bool) error {
	path, err := db.Path()
	if err != nil {
		return err
	}

	switch _, err := os.Stat(path); {
	case err == nil && !force:
		fmt.Printf("database already exists at %s\nnothing to do — use --force to back it up and create a new one\n", path)
		return nil
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
	fmt.Println("created database at", path)
	return nil
}
