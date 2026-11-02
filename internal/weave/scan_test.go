package weave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixture = "../../test/fixture"

// Node's walk of test/fixture: `walk('test/fixture').map(f => [f.rel, f.lines.length])`.
var fixtureWalk = []struct {
	rel   string
	lines int
}{
	{"api/openapi.json", 11},
	{"packages/backend/prisma/schema.prisma", 16},
	{"packages/backend/src/app.ts", 7},
	{"packages/backend/src/modules/legacy/legacy.ts", 4},
	{"packages/backend/src/modules/orders/orders.routes.ts", 13},
	{"packages/backend/src/trpc/router.ts", 15},
	{"packages/web/app/api/quotes/route.ts", 11},
	{"packages/web/e2e/orders.spec.ts", 8},
	{"packages/web/package.json", 6},
	{"packages/web/src/App.tsx", 13},
	{"packages/web/src/pages/Orders.tsx", 12},
	{"specs/changes/2026-01-01-orders/spec-delta.md", 6},
	{"specs/changes/2026-01-01-orders/tasks.md", 7},
}

func TestWalkFixture(t *testing.T) {
	files := Walk(fixture)
	if len(files) != len(fixtureWalk) {
		t.Fatalf("walked %d files, Node walks %d: %v", len(files), len(fixtureWalk), rels(files))
	}
	for i, f := range files {
		want := fixtureWalk[i]
		if f.Rel != want.rel || len(f.Lines) != want.lines {
			t.Errorf("file %d: %s (%d lines), Node %s (%d lines)", i, f.Rel, len(f.Lines), want.rel, want.lines)
		}
		if strings.Contains(f.Rel, "\\") || strings.Contains(f.Rel, ".weave") || strings.Contains(f.Rel, "node_modules") {
			t.Errorf("rel %q is not a POSIX source path", f.Rel)
		}
		if f.Abs != filepath.Join(fixture, filepath.FromSlash(f.Rel)) || f.Text == "" {
			t.Errorf("%s: abs %q, %d bytes", f.Rel, f.Abs, len(f.Text))
		}
	}
	if NewestMtime(fixture) <= 0 {
		t.Fatal("NewestMtime(fixture) <= 0")
	}
}

func TestWalkSkipsAndOrder(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, size int) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{
		"a-c.ts", "a/b.ts", "B.ts", "UP.TS", "notes.txt", "README", "x.d.ts", "\xef\xbc\xa1.ts", "\xf0\x9f\x98\x80.ts",
		"node_modules/m.ts", ".hidden/h.ts", ".dot.ts", "dist/d.js", "Pods/p.ts", "src/vendor/v.ts", "src/.weave/graph.json",
	} {
		write(rel, 10)
	}
	write("big.json", MaxBytes+1)
	write("edge.json", MaxBytes)

	// readdirSync(dir).sort(): UTF-16 order puts the emoji (D83D) before U+FF21.
	want := []string{"B.ts", "UP.TS", "a/b.ts", "a-c.ts", "edge.json", "x.d.ts", "\xf0\x9f\x98\x80.ts", "\xef\xbc\xa1.ts"}
	if got := rels(Walk(root)); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("walk = %v, want %v", got, want)
	}

	// Skipped trees do not count towards freshness.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "node_modules", "m.ts"), future, future); err != nil {
		t.Fatal(err)
	}
	if m := NewestMtime(root); m <= 0 || m >= float64(future.UnixMilli()) {
		t.Fatalf("NewestMtime = %v, counted a skipped file", m)
	}
}

func TestPathHelpers(t *testing.T) {
	eq := func(got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	eq(Ext("a/b.Test.TSX"), ".tsx")
	eq(Ext("Makefile"), "")
	eq(StripExt("a/b.test.ts"), "a/b.test")
	eq(StripExt("a/b"), "a/b")
	eq(DirOf("a/b/c.ts"), "a/b")
	eq(DirOf("c.ts"), "")
	eq(BaseName("a/b/c.ts"), "c.ts")
	eq(BaseName("c.ts"), "c.ts")
	eq(ToPosix(`a\b\c`), "a/b/c")
	// path.posix.join, checked against Node.
	eq(Rel("a", "", "b/../c"), "a/c")
	eq(Rel(""), ".")
	eq(Rel("a/"), "a/")
	eq(Rel("/", ".."), "/")
	eq(Rel("..", "a"), "../a")
	eq(Rel("./"), "./")
	eq(Rel("a", "..", ".."), "..")
	eq(Rel("a//b/", "./c/"), "a/b/c/")
	eq(Rel("/a/../../b"), "/b")
}

func rels(files []SourceFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Rel
	}
	return out
}
