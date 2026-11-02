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
	var did, skipped []string

	writeJSON := func(rel string, value any) {
		p := filepath.Join(root, rel)
		if pathExists(p) {
			skipped = append(skipped, rel+" (exists)")
			return
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			skipped = append(skipped, rel+" (unwritable)")
			return
		}
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			skipped = append(skipped, rel+" (encode)")
			return
		}
		if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
			skipped = append(skipped, rel+" (unwritable)")
			return
		}
		did = append(did, rel)
	}

	stdio := stdioConfig{Command: exe, Args: args}
	writeJSON(".mcp.json", mcpFile{MCPServers: map[string]stdioConfig{"weave": stdio}})
	writeJSON(filepath.Join(".cursor", "mcp.json"), mcpFile{MCPServers: map[string]stdioConfig{"weave": stdio}})
	writeJSON(filepath.Join(".vscode", "mcp.json"), vsFile{Servers: map[string]vsConfig{
		"weave": {Type: "stdio", Command: exe, Args: args},
	}})

	oc := filepath.Join(root, "opencode.json")
	if pathExists(oc) {
		raw, err := os.ReadFile(oc)
		var doc map[string]any
		if err != nil || json.Unmarshal(raw, &doc) != nil {
			skipped = append(skipped, "opencode.json (not valid JSON — add by hand)")
		} else {
			mcp, _ := doc["mcp"].(map[string]any)
			if mcp == nil {
				mcp = map[string]any{}
			}
			if _, exists := mcp["weave"]; exists {
				skipped = append(skipped, "opencode.json (already has weave)")
			} else {
				mcp["weave"] = map[string]any{
					"type":    "local",
					"command": []string{exe, "mcp", "--root", "."},
					"enabled": true,
				}
				doc["mcp"] = mcp
				b, _ := json.MarshalIndent(doc, "", "  ")
				if os.WriteFile(oc, append(b, '\n'), 0o644) == nil {
					did = append(did, "opencode.json")
				}
			}
		}
	} else {
		writeJSON("opencode.json", openCodeFile{Mcp: map[string]localConfig{
			"weave": {Type: "local", Command: []string{exe, "mcp", "--root", "."}, Enabled: true},
		}})
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
  git pre-commit   : "%s" check --root .
  claude code hook : {"matcher":"Edit|Write","hooks":[{"type":"command","command":"\"%s\" check --root ."}]}
  kiro hook        : .kiro/hooks/weave.json  -> trigger PostFileSave, command above
  ci               : "%s" index --root . && "%s" check --root .

mark code with the requirement it implements (exact traceability, any language):
  // @req REQ-ORDERS-3
  then context/impact report it, and a changed requirement marks the code suspect.

prove a requirement is tested (needed for `+"`weave evidence`"+` and the check gate):
  vitest     npx vitest run --reporter=junit --outputFile=.weave/junit.xml
  jest       npx jest --reporters=jest-junit   (JEST_JUNIT_OUTPUT_FILE=.weave/junit.xml)
  playwright npx playwright test --reporter=junit
  phpunit    vendor/bin/phpunit --log-junit .weave/junit.xml
  then: weave evidence
`, exe, exe, exe, exe, exe, exe)
	return 0
}
