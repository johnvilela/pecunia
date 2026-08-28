package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestAgentArgv(t *testing.T) {
	cases := []struct {
		agent string
		want  []string
	}{
		{"claude-code", []string{"claude", "mcp", "add", "--scope", "user", "pecunia", "--", "/bin/pecunia", "mcp"}},
		{"codex", []string{"codex", "mcp", "add", "pecunia", "--", "/bin/pecunia", "mcp"}},
		{"gemini", []string{"gemini", "mcp", "add", "--scope", "user", "pecunia", "/bin/pecunia", "mcp"}},
	}
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			got, err := agentArgv(c.agent, "/bin/pecunia")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("argv %q, want %q", got, c.want)
			}
		})
	}

	t.Run("unknown agent names the real ones", func(t *testing.T) {
		_, err := agentArgv("cursor", "/bin/pecunia")
		if err == nil || !strings.Contains(err.Error(), "opencode") {
			t.Fatalf("err %v — want the agent list", err)
		}
	})
}

func TestOpencodeMerge(t *testing.T) {
	entry := func(t *testing.T, out []byte) map[string]any {
		t.Helper()
		var cfg map[string]any
		if err := json.Unmarshal(out, &cfg); err != nil {
			t.Fatalf("merge produced bad JSON: %v\n%s", err, out)
		}
		mcp, _ := cfg["mcp"].(map[string]any)
		pecunia, _ := mcp["pecunia"].(map[string]any)
		if pecunia == nil {
			t.Fatalf("no mcp.pecunia in %s", out)
		}
		return pecunia
	}

	t.Run("fresh file", func(t *testing.T) {
		out, err := opencodeMerge(nil, "/bin/pecunia")
		if err != nil {
			t.Fatal(err)
		}
		k := entry(t, out)
		cmd, _ := k["command"].([]any)
		if k["type"] != "local" || len(cmd) != 2 || cmd[0] != "/bin/pecunia" || cmd[1] != "mcp" {
			t.Fatalf("entry %+v", k)
		}
	})

	t.Run("keeps unrelated keys and mcp siblings", func(t *testing.T) {
		existing := []byte(`{"theme": "dark", "mcp": {"other": {"type": "remote", "url": "https://x"}}}`)
		out, err := opencodeMerge(existing, "/bin/pecunia")
		if err != nil {
			t.Fatal(err)
		}
		entry(t, out)
		var cfg map[string]any
		if err := json.Unmarshal(out, &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg["theme"] != "dark" {
			t.Fatalf("theme lost: %s", out)
		}
		if _, ok := cfg["mcp"].(map[string]any)["other"]; !ok {
			t.Fatalf("mcp sibling lost: %s", out)
		}
	})

	t.Run("refuses to clobber JSONC", func(t *testing.T) {
		_, err := opencodeMerge([]byte("{\n// my settings\n}"), "/bin/pecunia")
		if err == nil {
			t.Fatal("comments were about to be clobbered")
		}
	})
}
