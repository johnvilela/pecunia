package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestSkillDests(t *testing.T) {
	got := skillDests("/h", "pecunia-budget")
	want := []string{
		"/h/.agents/skills/pecunia-budget/SKILL.md",
		"/h/.claude/skills/pecunia-budget/SKILL.md",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d destinations; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("destination %d is %q; want %q", i, got[i], want[i])
		}
	}
}

// skillName is the strictest naming rule on the roster (opencode's): it is
// what every agent will accept.
var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestSkillFiles(t *testing.T) {
	entries, err := skillFS.ReadDir("skills")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"pecunia-overview.md": true, "pecunia-budget.md": true,
		"pecunia-import.md": true, "pecunia-health.md": true,
		"pecunia-omni.md": true,
	}
	if len(entries) != len(want) {
		t.Fatalf("embedded %d skills; want %d", len(entries), len(want))
	}

	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			if !want[e.Name()] {
				t.Fatalf("unexpected skill file %q", e.Name())
			}
			data, err := skillFS.ReadFile("skills/" + e.Name())
			if err != nil {
				t.Fatal(err)
			}
			rest, ok := strings.CutPrefix(string(data), "---\n")
			if !ok {
				t.Fatal("file does not open with frontmatter")
			}
			front, body, ok := strings.Cut(rest, "\n---\n")
			if !ok {
				t.Fatal("frontmatter never closes")
			}

			base := strings.TrimSuffix(e.Name(), ".md")
			if !strings.Contains(front, "name: "+base+"\n") {
				t.Errorf("frontmatter name is not %q", base)
			}
			if !skillName.MatchString(base) {
				t.Errorf("name %q breaks the skill naming rule", base)
			}
			_, desc, ok := strings.Cut(front, "description: ")
			if !ok {
				t.Fatal("frontmatter has no description")
			}
			desc, _, _ = strings.Cut(desc, "\n")
			if len(desc) == 0 || len(desc) > 1024 {
				t.Errorf("description is %d chars; want 1-1024", len(desc))
			}

			// The two rules no skill may lose to an edit: it points at the MCP
			// tools, and it says amounts are in minor units.
			if !strings.Contains(body, "pecunia_") {
				t.Error("body never names an MCP tool")
			}
			if !strings.Contains(body, "minor units") {
				t.Error("body never states the minor-units rule")
			}
		})
	}
}
