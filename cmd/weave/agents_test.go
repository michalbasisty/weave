package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentBlockKeepsTheFileAndIsWrittenOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "CLAUDE.md")
	if got := writeAgentBlock(p, false); got != "(created)" {
		t.Fatalf("new file: %q", got)
	}
	os.WriteFile(p, []byte("# Mine\n\nkeep this\n"), 0o644)
	if got := writeAgentBlock(p, false); got != "+= weave section" {
		t.Fatalf("append: %q", got)
	}
	if got := writeAgentBlock(p, false); got != "kept" {
		t.Fatalf("second run must not touch it: %q", got)
	}
	// a stale section is replaced in place on --upgrade, the rest of the file kept
	raw, _ := os.ReadFile(p)
	stale := strings.Replace(string(raw), "Grep is still right", "old text", 1) + "\ntail\n"
	os.WriteFile(p, []byte(stale), 0o644)
	if got := writeAgentBlock(p, true); got != "(upgraded)" {
		t.Fatalf("upgrade: %q", got)
	}
	raw, _ = os.ReadFile(p)
	s := string(raw)
	if strings.Count(s, agentStart) != 1 || !strings.Contains(s, "keep this") || !strings.HasSuffix(s, "tail\n") || strings.Contains(s, "old text") {
		t.Fatalf("upgrade result:\n%s", s)
	}
}

func TestOpenCodePluginQuotesTheBinary(t *testing.T) {
	js := openCodePlugin(`C:\Program Files\weave\weave.exe`)
	if !strings.Contains(js, `const WEAVE = "C:\\Program Files\\weave\\weave.exe"`) {
		t.Fatalf("binary path not a JS string literal:\n%s", js)
	}
	for _, want := range []string{"session.idle", "check --root", "client.session.prompt", "out === last"} {
		if !strings.Contains(js, want) {
			t.Errorf("plugin lacks %q", want)
		}
	}
}
