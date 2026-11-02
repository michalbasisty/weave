package baseline

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

func fixtureGraphPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "test", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	g := weave.BuildGraph(root)
	p := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(p, g.MarshalJS(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGraphFromFile(t *testing.T) {
	g := GraphFromFile(fixtureGraphPath(t))
	if g == nil {
		t.Fatal("GraphFromFile returned nil for the fixture")
	}
	if g.Version != 3 {
		t.Errorf("version = %d, want 3", g.Version)
	}
	var names []string
	for _, n := range g.Nodes {
		if n.Kind == "endpoint" {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	want := []string{
		"DELETE /api/reports/:id",
		"GET /api/quotes",
		"GET /api/reports/:id",
		"GET /api/v1/orders",
		"MUTATION /api/trpc/quotes.add",
		"POST /api/quotes",
		"POST /api/v1/orders",
		"QUERY /api/trpc/ping",
		"QUERY /api/trpc/quotes.list",
	}
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Errorf("endpoints:\n%s\nwant:\n%s", strings.Join(names, "\n"), strings.Join(want, "\n"))
	}
}

func TestGraphFromFileRejects(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	empty := `"root":"","builtAt":"","fileCount":0,"nodes":[],"edges":[]`
	if g := GraphFromFile(write("v2.json", `{"version":2,`+empty+`}`)); g != nil {
		t.Errorf("version 2 loaded: %+v", g)
	}
	if g := GraphFromFile(write("bad.json", `{not json`)); g != nil {
		t.Errorf("invalid JSON loaded: %+v", g)
	}
	if g := GraphFromFile(filepath.Join(dir, "missing.json")); g != nil {
		t.Errorf("missing file loaded: %+v", g)
	}
}

func TestGraphAt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	routes := `import { Router } from 'express';
const router = Router();
router.get('/hello', (_req, res) => res.json({ ok: true }));
export default router;
`
	if err := os.WriteFile(filepath.Join(src, "orders.routes.ts"), []byte(routes), 0o644); err != nil {
		t.Fatal(err)
	}

	run(t, root, "init", "-q")
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.email=test@example.com", "-c", "user.name=Test", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "one")

	g := GraphAt(root, "HEAD")
	if g == nil {
		t.Fatal("GraphAt(HEAD) returned nil")
	}
	var names []string
	for _, n := range g.Nodes {
		if n.Kind == "endpoint" {
			names = append(names, n.Name)
		}
	}
	if strings.Join(names, ",") != "GET /hello" {
		t.Errorf("endpoints at HEAD = %v, want [GET /hello]", names)
	}
	if got := worktreeCount(t, root); got != 1 {
		t.Errorf("worktree list has %d entries after GraphAt, want 1:\n%s", got, run(t, root, "worktree", "list", "--porcelain"))
	}

	if g := GraphAt(root, "no-such-ref"); g != nil {
		t.Errorf("GraphAt(no-such-ref) = %+v, want nil", g)
	}
	if g := GraphAt(root, "  "); g != nil {
		t.Errorf("GraphAt(blank ref) = %+v, want nil", g)
	}
	if got := worktreeCount(t, root); got != 1 {
		t.Errorf("worktree list has %d entries after the failures, want 1", got)
	}
}

func worktreeCount(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(run(t, dir, "worktree", "list", "--porcelain"), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			n++
		}
	}
	return n
}

// run runs git in dir and returns its trimmed output. A failing command fails the test.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
