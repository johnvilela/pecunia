package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed skills/*.md
var skillFS embed.FS

// skillDests is where one skill lands. Two roots cover the whole agent
// roster: ~/.agents/skills is the shared standard path codex, gemini and
// opencode all read, and ~/.claude/skills is claude-code's own. opencode
// reads both, which is harmless — the copies are identical.
func skillDests(home, name string) []string {
	return []string{
		filepath.Join(home, ".agents", "skills", name, "SKILL.md"),
		filepath.Join(home, ".claude", "skills", name, "SKILL.md"),
	}
}

// installSkills writes every embedded skill into both roots. The files are
// pecunia-owned, so an existing copy is overwritten — that is what makes a
// re-run after an upgrade refresh them.
func installSkills() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	entries, err := skillFS.ReadDir("skills")
	if err != nil {
		return err
	}
	installed := 0
	for _, e := range entries {
		// pecunia-omni teaches the Telegram commands, which only exist where
		// pecunia runs as an Omni plugin — omni-skills ships it, setup does not.
		if e.Name() == "pecunia-omni.md" {
			continue
		}
		data, err := skillFS.ReadFile("skills/" + e.Name())
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		for _, dest := range skillDests(home, name) {
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dest, data, 0o644); err != nil {
				return err
			}
		}
		installed++
	}
	fmt.Fprintf(out, "installed %d skills to ~/.agents/skills and ~/.claude/skills\n", installed)
	return nil
}
