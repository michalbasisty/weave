package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordAndRead(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WEAVE_NO_USAGE", "0")
	Record(root, "context", 100, 6.6)
	if got := Read(root); len(got) != 1 || got[0].Tool != "context" || got[0].Tokens != 100 || got[0].Ms != 7 {
		t.Fatalf("round trip = %+v", got)
	}
	raw, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, `{"at":"`) || !strings.Contains(line, `Z","tool":"context","tokens":100,"ms":7}`) {
		t.Errorf("ledger line = %s", line)
	}

	t.Setenv("WEAVE_NO_USAGE", "1")
	Record(root, "impact", 5, 1)
	if got := Read(root); len(got) != 1 {
		t.Errorf("WEAVE_NO_USAGE=1 wrote: %+v", got)
	}
}

func TestReadSkipsTornAndKeepsLast5000(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("{torn\n")
	for i := 0; i < 5001; i++ {
		fmt.Fprintf(&b, `{"at":"2026-01-01T00:00:00.000Z","tool":"t%04d","tokens":%d,"ms":1}`+"\n", i, i)
	}
	if err := os.WriteFile(Path(root), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Read(root)
	if len(got) != 5000 {
		t.Fatalf("read %d entries, want 5000", len(got))
	}
	if got[0].Tool != "t0001" || got[len(got)-1].Tool != "t5000" {
		t.Errorf("window = %s..%s", got[0].Tool, got[len(got)-1].Tool)
	}
}

func TestGain(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WEAVE_NO_USAGE", "0")
	for _, u := range []struct {
		tool   string
		tokens int
	}{
		{"context", 100},
		{"context", 300},
		{"impact", 400},
		{"status", 200},
	} {
		Record(root, u.tool, u.tokens, 1)
	}
	want := "gain: 4 call(s) in the last 7 day(s)\n" +
		"tokens      total 1,000 · median 300 · p95 400\n" +
		"by tool     context 2 (400) · impact 1 (400) · status 1 (200)\n" +
		"ledger      " + Path(root)
	if got := Gain(root, 7); got != want {
		t.Errorf("gain:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestGainEmpty(t *testing.T) {
	root := t.TempDir()
	want := "gain: no usage recorded in the last 7 day(s)\n" +
		"  MCP tool calls append to " + Path(root) + "; run an agent against this repo, then retry."
	if got := Gain(root, 7); got != want {
		t.Errorf("empty gain = %q, want %q", got, want)
	}
	// The cutoff is clamped to one day; the message keeps the number asked for, as the TypeScript does.
	want = "gain: no usage recorded in the last 0 day(s)\n" +
		"  MCP tool calls append to " + Path(root) + "; run an agent against this repo, then retry."
	if got := Gain(root, 0); got != want {
		t.Errorf("gain 0 = %q, want %q", got, want)
	}
}
