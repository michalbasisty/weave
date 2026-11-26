package baseline

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

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
	cached, err := filepath.Glob(filepath.Join(root, ".weave", "baselines", "*.json"))
	if err != nil || len(cached) != 1 {
		t.Fatalf("baseline cache: %v %v", cached, err)
	}
	if again := GraphAt(root, "HEAD"); again == nil || len(again.Nodes) != len(g.Nodes) {
		t.Fatal("cached GraphAt returned a different graph")
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

// TestGraphAtLinkedCacheStaysBestEffort proves the cache is best effort: a
// baselines directory linked out of the repo cannot receive a baseline, and the
// freshly built graph is still returned with nothing written outside.
func TestGraphAtLinkedCacheStaysBestEffort(t *testing.T) {
	root := gitRepo(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirLink(t, outside, filepath.Join(root, ".weave", "baselines"))

	g := GraphAt(root, "HEAD")
	if g == nil {
		t.Fatal("GraphAt(HEAD) returned nil with an unwritable cache")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("cache written through a linked baselines directory: %v", entries)
	}
}

// TestLinkedCacheDirRefused checks reading, writing and pruning through a cache
// directory linked out of the repo: the outside file is never read and never
// removed, and the write never lands there.
func TestLinkedCacheDirRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	valid := (&weave.Graph{Version: weave.GraphVersion, Nodes: []*weave.GNode{}, Edges: []weave.GEdge{}}).MarshalJS()
	if err := os.WriteFile(filepath.Join(outside, "read-v1.json"), valid, 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		name := filepath.Join(outside, "keep"+itoa(i)+".json")
		if err := os.WriteFile(name, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirLink(t, outside, filepath.Join(root, ".weave", "baselines"))

	if g := loadCache(root, "read-v1.json"); g != nil {
		t.Errorf("loadCache read through a linked cache directory: %+v", g)
	}
	if err := saveCache(root, "new-v1.json", []byte("{}")); err == nil {
		t.Error("saveCache wrote through a linked cache directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "new-v1.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a cache file was written outside the repo: %v", err)
	}

	prune(root, 1)

	for i := 0; i < 3; i++ {
		name := filepath.Join(outside, "keep"+itoa(i)+".json")
		if _, err := os.Stat(name); err != nil {
			t.Errorf("prune removed %s outside the repo: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "read-v1.json")); err != nil {
		t.Errorf("prune removed the outside cache file: %v", err)
	}
}

// TestPruneKeepsNewestTwenty pins the retention: the twenty newest .json cache
// files survive and the oldest is removed.
func TestPruneKeepsNewestTwenty(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".weave", "baselines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 21; i++ {
		name := filepath.Join(dir, "g"+itoa(i)+"-v1.json")
		if err := os.WriteFile(name, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		at := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, at, at); err != nil {
			t.Fatal(err)
		}
	}

	prune(root, 20)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Errorf("prune left %d cache files, want 20", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dir, "g0-v1.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("oldest cache file survived prune: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "g1-v1.json")); err != nil {
		t.Errorf("prune removed a file within retention: %v", err)
	}
}

// gitRepo makes a one-commit repository with an endpoint and skips when git is
// missing.
func gitRepo(t *testing.T) string {
	t.Helper()
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
	return root
}

// dirLink points link at target. A symlink is used where the OS allows it; on
// Windows, where os.Symlink needs a privilege, a directory junction stands in. A
// Unix test that cannot make its symlink fails instead of skipping: the link is the
// thing under test there.
func dirLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	} else if runtime.GOOS != "windows" {
		t.Fatalf("create a symlink, which this Unix test requires: %v", err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction: %v: %s", err, strings.TrimSpace(string(out)))
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
