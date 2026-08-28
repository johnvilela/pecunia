// pecunia mcp install registers the MCP server with an AI agent, so "pecunia mcp"
// is one command away from being usable instead of a config-file scavenger
// hunt. Agents with an "mcp add" of their own get shelled out to — their CLI
// owns their config format; only OpenCode has no such command, so its JSON is
// merged directly.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"pecunia/internal/core"
)

const agentList = "claude-code, codex, gemini, opencode"

// agentArgv is the registration command for the agents that have one. The
// binary is the absolute path of the running pecunia, so a dev build registers
// the dev binary and its isolated database, which is honest.
func agentArgv(agent, exe string) ([]string, error) {
	switch agent {
	case "claude-code":
		return []string{"claude", "mcp", "add", "--scope", "user", "pecunia", "--", exe, "mcp"}, nil
	case "codex":
		return []string{"codex", "mcp", "add", "pecunia", "--", exe, "mcp"}, nil
	case "gemini":
		return []string{"gemini", "mcp", "add", "--scope", "user", "pecunia", exe, "mcp"}, nil
	}
	return nil, fmt.Errorf("unknown agent %q — one of %s", agent, agentList)
}

var errJSONC = errors.New("the config is not plain JSON (comments?), and pecunia will not clobber it")

// opencodeMerge adds the pecunia entry to an opencode.json, keeping everything
// already in it. OpenCode allows JSONC, which encoding/json cannot round-trip
// without eating the comments — that case is refused, never rewritten.
func opencodeMerge(existing []byte, exe string) ([]byte, error) {
	cfg := map[string]any{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return nil, errJSONC
		}
	}
	mcp, _ := cfg["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
		cfg["mcp"] = mcp
	}
	mcp["pecunia"] = map[string]any{
		"type":    "local",
		"command": []string{exe, "mcp"},
		"enabled": true,
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func opencodeSnippet(exe string) string {
	return fmt.Sprintf(`  "mcp": {
    "pecunia": {
      "type": "local",
      "command": [%q, "mcp"],
      "enabled": true
    }
  }`, exe)
}

func installOpencode(exe string) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "opencode", "opencode.json")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	merged, err := opencodeMerge(existing, exe)
	if err != nil {
		fmt.Fprintf(out, "%s: %v\nadd this to it yourself:\n%s\n", path, err, opencodeSnippet(exe))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, merged, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "registered pecunia in %s\n", path)
	return nil
}

func runMCPInstall(args []string) error {
	agents := []core.Choice{
		{Label: "claude-code", Desc: "Claude Code"},
		{Label: "codex", Desc: "OpenAI Codex CLI"},
		{Label: "gemini", Desc: "Gemini CLI"},
		{Label: "opencode", Desc: "OpenCode"},
	}
	var agent string
	if len(args) > 0 {
		agent = args[0]
	} else {
		picked, err := core.Pick(agents, "Install the pecunia MCP server into which agent?",
			func(c core.Choice) core.Choice { return c })
		if err != nil {
			return err
		}
		agent = picked.Label
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if agent == "opencode" {
		return installOpencode(exe)
	}
	argv, err := agentArgv(agent, exe)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return fmt.Errorf("%s not found on PATH — is %s installed?", argv[0], agent)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	fmt.Fprintf(out, "registered pecunia with %s\n", agent)
	return nil
}
