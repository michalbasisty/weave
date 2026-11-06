package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type stdioConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type mcpFile struct {
	MCPServers map[string]stdioConfig `json:"mcpServers"`
}

type vsConfig struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type vsFile struct {
	Servers map[string]vsConfig `json:"servers"`
}

type localConfig struct {
	Type    string   `json:"type"`
	Command []string `json:"command"`
	Enabled bool     `json:"enabled"`
}

type openCodeFile struct {
	Mcp map[string]localConfig `json:"mcp"`
}

var weaveLine = regexp.MustCompile(`(?m)^\.weave/?$`)

func cmdInit() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	args := []string{"mcp", "--root", "."}
	upgrade := hasFlag("upgrade")
	want, bad := initTargets(flagValues("only"))
	if bad != "" {
		fmt.Fprintf(os.Stderr, "weave init: unknown --only target %q (use claude, cursor, vscode, opencode)\n", bad)
		return 2
	}
	var did, skipped []string

	writeValue := func(p string, value any) bool {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return false
		}
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return false
		}
		return os.WriteFile(p, append(b, '\n'), 0o644) == nil
	}
	readDoc := func(p string) (map[string]any, bool) {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, false
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			return nil, false
		}
		return doc, true
	}

	// ensure writes a missing config, or replaces the weave entry when --upgrade
	// is asked for. Without --upgrade an existing file is never touched.
	ensure := func(rel string, value any, apply func(map[string]any)) {
		p := filepath.Join(root, rel)
		if !pathExists(p) {
			if writeValue(p, value) {
				did = append(did, rel)
			} else {
				skipped = append(skipped, rel+" (unwritable)")
			}
			return
		}
		if !upgrade {
			skipped = append(skipped, rel+" (exists)")
			return
		}
		doc, ok := readDoc(p)
		if !ok {
			skipped = append(skipped, rel+" (not valid JSON — add by hand)")
			return
		}
		apply(doc)
		if writeValue(p, doc) {
			did = append(did, rel+" (upgraded)")
		} else {
			skipped = append(skipped, rel+" (unwritable)")
		}
	}

	setMCP := func(doc map[string]any) {
		servers, _ := doc["mcpServers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
		}
		servers["weave"] = map[string]any{"command": exe, "args": args}
		doc["mcpServers"] = servers
	}
	if want["claude"] {
		ensure(".mcp.json", mcpFile{MCPServers: map[string]stdioConfig{"weave": {Command: exe, Args: args}}}, setMCP)
	}
	if want["cursor"] {
		ensure(filepath.Join(".cursor", "mcp.json"), mcpFile{MCPServers: map[string]stdioConfig{"weave": {Command: exe, Args: args}}}, setMCP)
	}
	if want["vscode"] {
		ensure(filepath.Join(".vscode", "mcp.json"), vsFile{Servers: map[string]vsConfig{
			"weave": {Type: "stdio", Command: exe, Args: args},
		}}, func(doc map[string]any) {
			servers, _ := doc["servers"].(map[string]any)
			if servers == nil {
				servers = map[string]any{}
			}
			servers["weave"] = map[string]any{"type": "stdio", "command": exe, "args": args}
			doc["servers"] = servers
		})
	}

	oc := filepath.Join(root, "opencode.json")
	if !want["opencode"] {
		// not asked for
	} else if !pathExists(oc) {
		if writeValue(oc, openCodeFile{Mcp: map[string]localConfig{
			"weave": {Type: "local", Command: []string{exe, "mcp", "--root", "."}, Enabled: true},
		}}) {
			did = append(did, "opencode.json")
		} else {
			skipped = append(skipped, "opencode.json (unwritable)")
		}
	} else if doc, ok := readDoc(oc); !ok {
		skipped = append(skipped, "opencode.json (not valid JSON — add by hand)")
	} else {
		mcp, _ := doc["mcp"].(map[string]any)
		if mcp == nil {
			mcp = map[string]any{}
		}
		_, has := mcp["weave"]
		if has && !upgrade {
			skipped = append(skipped, "opencode.json (already has weave)")
		} else {
			mcp["weave"] = map[string]any{
				"type":    "local",
				"command": []string{exe, "mcp", "--root", "."},
				"enabled": true,
			}
			doc["mcp"] = mcp
			if writeValue(oc, doc) {
				if has {
					did = append(did, "opencode.json (upgraded)")
				} else {
					did = append(did, "opencode.json")
				}
			}
		}
	}

	// The Stop hook is the one place the agent cannot skip the gate: exit 2 keeps the
	// turn going and hands stderr back to the model.
	if hasFlag("hooks") {
		rel := filepath.Join(".claude", "settings.json")
		p := filepath.Join(root, rel)
		doc, ok := readDoc(p)
		if !ok {
			doc = map[string]any{}
		}
		hooks, _ := doc["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
		}
		stop, _ := hooks["Stop"].([]any)
		cmdLine := `"` + exe + `" hook --root .`
		present := false
		for _, e := range stop {
			m, _ := e.(map[string]any)
			inner, _ := m["hooks"].([]any)
			for _, h := range inner {
				hm, _ := h.(map[string]any)
				if s, _ := hm["command"].(string); strings.Contains(s, "hook") && strings.Contains(s, "weave") {
					present = true
				}
			}
		}
		if !present {
			stop = append(stop, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmdLine}}})
		}
		hooks["Stop"] = stop
		doc["hooks"] = hooks
		if writeValue(p, doc) {
			if present {
				skipped = append(skipped, rel+" (hook already there)")
			} else {
				did = append(did, rel+" += Stop hook")
			}
		} else {
			skipped = append(skipped, rel+" (unwritable)")
		}
	}

	gi := filepath.Join(root, ".gitignore")
	if pathExists(gi) {
		text, err := os.ReadFile(gi)
		if err == nil && !weaveLine.Match(text) {
			s := strings.TrimRight(string(text), "\n")
			if err := os.WriteFile(gi, []byte(s+"\n.weave/\n"), 0o644); err == nil {
				did = append(did, ".gitignore += .weave/")
			}
		}
	} else if os.WriteFile(gi, []byte(".weave/\n"), 0o644) == nil {
		did = append(did, ".gitignore")
	}

	fmt.Printf("weave init in %s\n", root)
	for _, d := range did {
		fmt.Printf("  + %s\n", d)
	}
	for _, s := range skipped {
		fmt.Printf("  = %s\n", s)
	}
	fmt.Printf(`
claude code (user scope, any project):
  claude mcp add weave -- "%s" mcp --root .

serve every registered project from one MCP entry (no per-repo config):
  claude mcp add weave -- "%s" mcp --all
  then: context/plan/impact take an optional "project" argument (names from `+"`weave projects`"+`).

agents that read AGENTS.md, paste this:
  ## Repo map
  Before editing any file, call weave's context(files) once. It returns the page,
  endpoints, tables, owning spec change, open tasks, the tests to run and the
  command to run them. Use impact(files) for the full blast radius.

enforce it without the agent's cooperation (any of these, same command):
  claude code      : weave init --hooks   (writes a Stop hook that runs the gate)
  git pre-commit   : "%s" check --root .
  kiro hook        : .kiro/hooks/weave.json  -> trigger PostFileSave, same command
  ci               : "%s" index --root . && "%s" check --root .

what the hook has seen (after a week: if it never fires, it is not earning its place):
  weave fires --since 7

mark code with the requirement it implements (exact traceability, any language):
  // @req REQ-ORDERS-3
  then context/impact report it, and a changed requirement marks the code suspect.

prove a requirement is tested (needed for `+"`weave evidence`"+` and the check gate):
  vitest     npx vitest run --reporter=junit --outputFile=.weave/junit.xml
  jest       npx jest --reporters=jest-junit   (JEST_JUNIT_OUTPUT_FILE=.weave/junit.xml)
  playwright npx playwright test --reporter=junit
  phpunit    vendor/bin/phpunit --log-junit .weave/junit.xml
  then: weave evidence
`, exe, exe, exe, exe, exe)
	return 0
}

// initTargets reads `--only claude,cursor` (repeatable). No flag means every client.
func initTargets(only []string) (map[string]bool, string) {
	all := []string{"claude", "cursor", "vscode", "opencode"}
	want := map[string]bool{}
	if len(only) == 0 {
		for _, t := range all {
			want[t] = true
		}
		return want, ""
	}
	for _, v := range only {
		for _, t := range strings.Split(v, ",") {
			t = strings.ToLower(strings.TrimSpace(t))
			switch t {
			case "":
			case "claude", "cursor", "vscode", "opencode":
				want[t] = true
			case "code", "vs-code":
				want["vscode"] = true
			default:
				return nil, t
			}
		}
	}
	return want, ""
}
