package grep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michalbasisty/weave/internal/weave"
)

func TestSearcherReReadsOnlyChangedFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.ts")
	if err := os.WriteFile(file, []byte("export const one = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := weave.BuildGraph(dir)
	g.Root = dir

	if out := Grep(g, "one", 5, false); !strings.Contains(out, "a.ts:1") {
		t.Fatalf("first search: %s", out)
	}
	if err := os.WriteFile(file, []byte("export const two = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(file, future, future); err != nil {
		t.Fatal(err)
	}
	if out := Grep(g, "two", 5, false); !strings.Contains(out, "a.ts:1") {
		t.Fatalf("changed file was not re-read: %s", out)
	}
	if out := Grep(g, "one", 5, false); !strings.Contains(out, "no match") {
		t.Fatalf("stale line survived: %s", out)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if out := Grep(g, "two", 5, false); !strings.Contains(out, "no match") {
		t.Fatalf("deleted file survived: %s", out)
	}
}
