package verify

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michalbasisty/weave/internal/weave"
)

// A commit that removed `users/:id` was reported as breaking
// `api.get('/reports${params}')`: the endpoint's parameter matched an unrelated
// single-segment path. A match needs a literal segment in common.
func TestSameShapeNeedsALiteralSegment(t *testing.T) {
	cases := []struct {
		call, endpoint string
		want           bool
	}{
		{"GET /reports", "GET /api/v1/users/:id", false},
		{"GET /stats", "GET /api/v1/users/:id", false},
		{"GET /tasks/:id", "GET /api/v1/users/:id", false},
		{"GET /tasks/42", "GET /api/v1/tasks/:id", true},
		{"GET /api/v1/tasks/42", "GET /api/v1/tasks/:id", true},
		{"GET /api/v1/tasks", "GET /api/v1/tasks", true},
		{"DELETE /api/v1/tasks/:id", "GET /api/v1/tasks/:id", false},
	}
	for _, c := range cases {
		if got := sameShape(c.endpoint, c.call); got != c.want {
			t.Errorf("sameShape(%q, %q) = %v, want %v", c.endpoint, c.call, got, c.want)
		}
	}
}

// An endpoint that is gone but still called is the gate's one hard failure.
func TestRemovedEndpointStillCalledFails(t *testing.T) {
	root := t.TempDir()
	file, line := "packages/web/src/orders.tsx", 12
	g := &weave.Graph{
		Root: root,
		Nodes: []*weave.GNode{
			{ID: "call1", Kind: weave.KindCall, Name: "GET /api/v1/users/42", File: &file, Line: &line},
		},
		PrevEndpoints: []string{"GET /api/v1/users/:id"},
	}
	res := Run(g, root, "", false, nil)

	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "removed endpoint still called: GET /api/v1/users/:id from packages/web/src/orders.tsx:12") {
		t.Fatalf("errors = %v", res.Errors)
	}
	text := res.Text()
	if !strings.HasPrefix(text, "check: 0 endpoint(s) indexed, no changed file named, 1 endpoint(s) removed\n  FAIL ") {
		t.Fatalf("text:\n%s", text)
	}
	if strings.Count(text, "\n") != 1 {
		t.Fatalf("a single failure is one line after the scope:\n%s", text)
	}
}

// A diff the gate has nothing to say about still returns the commands that prove it.
func TestCleanDiffHasNoErrorsAndCommands(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	res := Run(g, root, "", false, []string{"packages/backend/src/modules/orders/orders.routes.ts"})

	if len(res.Errors) != 0 {
		t.Fatalf("errors = %v", res.Errors)
	}
	if len(res.Commands) == 0 {
		t.Fatalf("no commands: %+v", res)
	}
	text := res.Text()
	if !strings.HasPrefix(text, "check: 1 file(s) → 2 endpoint(s)") {
		t.Fatalf("scope:\n%s", text)
	}
	if !strings.Contains(text, "\n  run  ") {
		t.Fatalf("commands missing from the text:\n%s", text)
	}
}

// strict is the only switch: the same gap is a warning or a failure.
func TestStrictTurnsAnUntestedEndpointIntoAFailure(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	files := []string{"packages/backend/src/modules/orders/orders.routes.ts"}

	lenient := Run(g, root, "", false, files)
	strict := Run(g, root, "", true, files)
	if len(lenient.Errors) != 0 {
		t.Fatalf("lenient errors = %v", lenient.Errors)
	}
	if len(strict.Errors) == 0 || !strings.Contains(strict.Text(), "FAIL ") {
		t.Fatalf("strict did not fail:\n%s", strict.Text())
	}
}

// The PR comment names the removed endpoint, its caller and the screens that reach it.
func TestMarkdownNamesScreensOfABrokenCall(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	// The backend dropped GET /api/v1/orders; Orders.tsx still calls it.
	gone, file, line := "GET /api/v1/orders", "packages/web/src/pages/Orders.tsx", 9
	kept := g.Nodes[:0]
	for _, n := range g.Nodes {
		if !(n.Kind == weave.KindEndpoint && n.Name == gone) {
			kept = append(kept, n)
		}
	}
	g.Nodes = append(kept, &weave.GNode{ID: weave.CallID("GET", "/api/v1/orders"), Kind: weave.KindCall, Name: gone, File: &file, Line: &line})
	g.PrevEndpoints = []string{gone}

	res := Run(g, root, "", false, []string{"README.md"})
	if len(res.Broken) == 0 {
		t.Fatalf("nothing broken: %+v", res)
	}
	md := res.Markdown()
	if !strings.HasPrefix(md, Marker+"\n### ❌ weave check") || !strings.Contains(md, "`"+gone+"`") {
		t.Fatalf("markdown:\n%s", md)
	}
	if len(res.Broken[0].Screens) == 0 || !strings.Contains(md, "`"+res.Broken[0].Screens[0]+"`") {
		t.Fatalf("no screens for %s:\n%s", gone, md)
	}
	t.Log("\n" + md)
}

// Option-like, control-character and blank refs are refused before git sees
// them; they must never be rewritten into something else.
func TestUnusableAgainstRefIsRejected(t *testing.T) {
	root := t.TempDir()
	g := &weave.Graph{Root: root}
	for _, ref := range []string{"--main", "-b", "   ", "\x00bad", "line\nbreak"} {
		res := Run(g, root, ref, false, []string{"a.ts"})
		if res.Code() != 2 || len(res.Operational) == 0 || len(res.Errors) != 0 {
			t.Errorf("Run(%q): code %d, %+v", ref, res.Code(), res)
			continue
		}
		if !strings.Contains(res.Text(), "not a usable git ref") {
			t.Errorf("Run(%q) text = %q", ref, res.Text())
		}
	}
}

// A malformed ref that is not option-like is passed to git verbatim. git decides
// it is unreadable, which is operational; it is never sanitised into another ref.
func TestMalformedAgainstRefIsOperational(t *testing.T) {
	root := t.TempDir()
	g := &weave.Graph{Root: root, PrevEndpoints: []string{"GET /api/v1/users/:id"}}
	for _, ref := range []string{"origin/ma!in", "main:path", "main@{1}"} {
		res := Run(g, root, ref, false, []string{"a.ts"})
		if res.Code() != 2 || len(res.Operational) == 0 || len(res.Errors) != 0 {
			t.Errorf("Run(%q): code %d, %+v", ref, res.Code(), res)
		}
	}
}

// The same rule applies when the caller names files: the baseline ref is still
// validated verbatim before it is used.
func TestMalformedAgainstIsRejectedWithExplicitFiles(t *testing.T) {
	root := t.TempDir()
	g := &weave.Graph{Root: root, PrevEndpoints: []string{"GET /api/v1/users/:id"}}
	res := Run(g, root, "--main", false, []string{"packages/web/src/orders.tsx"})
	if res.Code() != 2 || len(res.Operational) == 0 || len(res.Errors) != 0 {
		t.Fatalf("code = %d, %+v", res.Code(), res)
	}
	if !strings.Contains(res.Text(), "not a usable git ref") {
		t.Fatalf("text = %q", res.Text())
	}
}

// A valid ref is still used untouched, including when files are explicit.
func TestValidAgainstRefIsNotRejected(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write("backend/tags.ts", "router.get('/tags', (req, res) => res.json([]))\n")
	write(".gitignore", ".weave/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")

	g, err := weave.Reindex(root)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(g, root, "HEAD", false, []string{"backend/tags.ts"})
	if len(res.Operational) != 0 {
		t.Fatalf("valid ref HEAD rejected: %v", res.Operational)
	}
	if res.Code() != 0 {
		t.Fatalf("code = %d:\n%s", res.Code(), res.Text())
	}
}

// An explicit --against ref the gate cannot read is operational even when the
// changed files are named: the gate must never fall back and call it a pass.
func TestUnreadableAgainstIsOperationalWithExplicitFiles(t *testing.T) {
	root := t.TempDir()
	g := &weave.Graph{Root: root, PrevEndpoints: []string{"GET /api/v1/users/:id"}}
	res := Run(g, root, "no-such-ref", false, []string{"packages/web/src/orders.tsx"})

	if res.Code() != 2 {
		t.Fatalf("code = %d, want 2: %+v", res.Code(), res)
	}
	if len(res.Operational) == 0 || len(res.Errors) != 0 {
		t.Fatalf("operational = %v, errors = %v", res.Operational, res.Errors)
	}
	text := res.Text()
	if !strings.Contains(text, "  error ") || strings.Contains(text, "FAIL ") {
		t.Fatalf("text did not distinguish operational from contract:\n%s", text)
	}
	md := res.Markdown()
	if strings.Contains(md, "passed") || !strings.Contains(md, "could not run") {
		t.Fatalf("markdown claimed a verdict:\n%s", md)
	}
}

// An --against ref from an unrelated history has no merge base, so no diff can be
// read against it and no removal can be judged. The gate must say so and exit 2,
// even when files are named explicitly, and the markdown must not borrow a pass.
func TestUnrelatedHistoryAgainstIsOperational(t *testing.T) {
	root, git := gitRepo(t)
	writeRepoFile(t, root, "backend/tags.ts", "router.get('/tags', (req, res) => res.json([]))\n")
	writeRepoFile(t, root, ".gitignore", ".weave/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")
	git("branch", "-f", "base-line", "HEAD")
	// A second root commit: no parent in common with base-line.
	git("checkout", "-q", "--orphan", "side")
	git("commit", "-qm", "unrelated root")
	git("checkout", "-q", "base-line")

	g, err := weave.Reindex(root)
	if err != nil {
		t.Fatal(err)
	}
	// A ref that does share history is still gated normally.
	if res := Run(g, root, "base-line", false, []string{"backend/tags.ts"}); res.Code() != 0 {
		t.Fatalf("related ref code = %d:\n%s", res.Code(), res.Text())
	}

	for _, files := range [][]string{{"backend/tags.ts"}, nil} {
		res := Run(g, root, "side", false, files)
		if res.Code() != 2 || len(res.Operational) == 0 || len(res.Errors) != 0 {
			t.Fatalf("files=%v: code %d, %+v", files, res.Code(), res)
		}
		if !strings.Contains(res.Text(), "no merge base") {
			t.Errorf("files=%v: text = %q", files, res.Text())
		}
		md := res.Markdown()
		if strings.Contains(md, "passed") || !strings.Contains(md, "could not run") {
			t.Errorf("files=%v: markdown claimed a verdict:\n%s", files, md)
		}
	}
}

// Explicit files need no git at all — including when git is not installed.
func TestExplicitFilesWorkWithoutGit(t *testing.T) {
	root := t.TempDir()
	g := &weave.Graph{Root: root}
	t.Setenv("PATH", t.TempDir())
	res := Run(g, root, "", false, []string{"a.ts"})
	if len(res.Operational) != 0 {
		t.Fatalf("operational = %v", res.Operational)
	}
	if res.Code() != 0 {
		t.Fatalf("code = %d: %+v", res.Code(), res)
	}
}

// Without git the gate cannot discover a change set at all; that is operational,
// never a green "no changed file named".
func TestMissingGitIsOperational(t *testing.T) {
	root := t.TempDir()
	g := &weave.Graph{Root: root}
	t.Setenv("PATH", t.TempDir())
	res := Run(g, root, "", false, nil)
	if res.Code() != 2 {
		t.Fatalf("code = %d, want 2: %+v", res.Code(), res)
	}
	if len(res.Operational) == 0 || len(res.Errors) != 0 {
		t.Fatalf("operational = %v, errors = %v", res.Operational, res.Errors)
	}
	if !strings.Contains(res.Text(), "  error ") {
		t.Fatalf("text = %q", res.Text())
	}
}

// A --root that names a subdirectory must see paths relative to that root, both
// for the working tree and for a committed range, so JUnit freshness can stat them.
func TestDiscoverChangesFromSubdirectory(t *testing.T) {
	root, git := gitRepo(t)
	writeRepoFile(t, root, "sub/mod.ts", "// one\n")
	writeRepoFile(t, root, "top.ts", "// top\n")
	writeRepoFile(t, root, ".gitignore", ".weave/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")

	writeRepoFile(t, root, "sub/mod.ts", "// two\n")
	writeRepoFile(t, root, "sub/new.ts", "// new\n")
	sub := filepath.Join(root, "sub")

	got, err := discoverChanges(sub, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mod.ts", "new.ts"}; !sameStrings(got, want) {
		t.Fatalf("local paths = %q, want %q", got, want)
	}

	git("add", "-A")
	git("commit", "-qm", "change")
	got, err = discoverChanges(sub, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mod.ts", "new.ts"}; !sameStrings(got, want) {
		t.Fatalf("range paths = %q, want %q", got, want)
	}
}

// A valid Unicode branch must reach git untouched. Refusing it or stripping the
// non-ASCII characters would make a legitimate checkout unusable.
func TestValidUnicodeAgainstRefWorks(t *testing.T) {
	root, git := gitRepo(t)
	writeRepoFile(t, root, "backend/tags.ts", "router.get('/tags', (req, res) => res.json([]))\n")
	writeRepoFile(t, root, ".gitignore", ".weave/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")
	branch := "zażółć"
	git("branch", branch)

	g, err := weave.Reindex(root)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(g, root, branch, false, []string{"backend/tags.ts"})
	if len(res.Operational) != 0 {
		t.Fatalf("Unicode ref rejected: %v", res.Operational)
	}
	if res.Code() != 0 {
		t.Fatalf("code = %d:\n%s", res.Code(), res.Text())
	}
	// The ref also has to survive change discovery, not only the baseline worktree.
	res = Run(g, root, branch, false, nil)
	if len(res.Operational) != 0 {
		t.Fatalf("Unicode ref failed discovery: %v", res.Operational)
	}
	if res.Code() != 0 {
		t.Fatalf("discovery code = %d:\n%s", res.Code(), res.Text())
	}
}

// `release` exists, so the old sanitiser turned `relea!se` into it and could show
// a pass. The malformed spelling must stay malformed and fail operationally.
func TestMalformedAgainstRefIsNotSilentlyRewritten(t *testing.T) {
	root, git := gitRepo(t)
	writeRepoFile(t, root, "backend/tags.ts", "router.get('/tags', (req, res) => res.json([]))\n")
	writeRepoFile(t, root, ".gitignore", ".weave/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")
	git("branch", "release")

	g, err := weave.Reindex(root)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(g, root, "relea!se", false, []string{"backend/tags.ts"})
	if res.Code() != 2 || len(res.Operational) == 0 || len(res.Errors) != 0 {
		t.Fatalf("malformed ref was treated as release: code %d, %+v", res.Code(), res)
	}
}

// Local discovery reads the whole working tree: a tracked edit alone used to hide
// an untracked file, and a quoted path could not survive line splitting.
func TestDiscoverChangesIncludesTrackedAndUntracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write("tracked file.ts", "// one\n")
	write(".gitignore", ".weave/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")

	write("tracked file.ts", "// two\n")
	write("new file.ts", "// new\n")

	got, err := discoverChanges(root, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"tracked file.ts": true, "new file.ts": true}
	if len(got) != len(want) {
		t.Fatalf("files = %q, want %v", got, want)
	}
	for _, f := range got {
		if !want[f] {
			t.Fatalf("unexpected file %q (NUL-safe paths must survive intact): %q", f, got)
		}
	}
}

func TestStatusPathsHandlesNULAndRenames(t *testing.T) {
	out := " M tracked file.ts\x00?? new file.ts\x00?? line\nbreak.ts\x00R  renamed.ts\x00old name.ts\x00"
	got := statusPaths(out)
	want := []string{"tracked file.ts", "new file.ts", "line\nbreak.ts", "renamed.ts"}
	if len(got) != len(want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A report older than the changed source must not turn its failure into a gate
// failure, nor its passes into proof; the gate warns that evidence was not checked.
func TestStaleJUnitDoesNotAssertPassOrFail(t *testing.T) {
	root := copyFixture(t)
	g := weave.BuildGraph(root)
	if g == nil {
		t.Fatal("no graph")
	}
	reportPath := filepath.Join(root, ".weave", "junit.xml")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	junitXML := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites><testsuite name="orders">
  <testcase classname="orders" name="REQ-ORD-1 orders page loads">
    <failure message="boom">expected 1, got 0</failure>
  </testcase>
</testsuite></testsuites>`
	if err := os.WriteFile(reportPath, []byte(junitXML), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "packages", "backend", "src", "modules", "orders", "orders.routes.ts")
	old := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-time.Hour)
	if err := os.Chtimes(reportPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, newer, newer); err != nil {
		t.Fatal(err)
	}

	files := []string{"packages/backend/src/modules/orders/orders.routes.ts"}
	res := Run(g, root, "", false, files)
	if res.Code() != 2 {
		t.Fatalf("stale report was green or a contract failure: code %d\n%s", res.Code(), res.Text())
	}
	if len(res.Errors) != 0 {
		t.Fatalf("stale report asserted a contract failure: %v", res.Errors)
	}
	found := false
	for _, e := range res.Operational {
		if strings.Contains(e, "predates the changed source") && strings.Contains(e, "stale") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no stale diagnostic: %v", res.Operational)
	}
	if text := res.Text(); !strings.Contains(text, "  error ") || strings.Contains(text, "requirement test failing") {
		t.Fatalf("text = %q", text)
	}

	// The same report, written after the source, is evidence again.
	fresh := time.Now().Add(time.Hour)
	if err := os.Chtimes(reportPath, fresh, fresh); err != nil {
		t.Fatal(err)
	}
	res = Run(g, root, "", false, files)
	if res.Code() != 1 || !strings.Contains(res.Text(), "requirement test failing") {
		t.Fatalf("fresh report was not read: %+v\n%s", res, res.Text())
	}
}

// A stale report with passing tests is not proof either: it must not turn the
// gate green, because the source moved after the tests ran.
func TestStalePassingJUnitIsNotGreen(t *testing.T) {
	root := copyFixture(t)
	g := weave.BuildGraph(root)
	if g == nil {
		t.Fatal("no graph")
	}
	reportPath := filepath.Join(root, ".weave", "junit.xml")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	junitXML := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites><testsuite name="orders">
  <testcase classname="orders" name="REQ-ORD-1 orders page loads"/>
</testsuite></testsuites>`
	if err := os.WriteFile(reportPath, []byte(junitXML), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "packages", "backend", "src", "modules", "orders", "orders.routes.ts")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(reportPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	res := Run(g, root, "", false, []string{"packages/backend/src/modules/orders/orders.routes.ts"})
	if res.Code() != 2 {
		t.Fatalf("stale passing report yielded code %d:\n%s", res.Code(), res.Text())
	}
	for _, e := range res.Operational {
		if strings.Contains(e, "stale") {
			return
		}
	}
	t.Fatalf("no stale diagnostic: %v", res.Operational)
}

func gitRepo(t *testing.T) (string, func(args ...string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root, git
}

func writeRepoFile(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	count := make(map[string]int, len(got))
	for _, s := range got {
		count[s]++
	}
	for _, s := range want {
		if count[s] == 0 {
			return false
		}
		count[s]--
	}
	return true
}

func copyFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "test", "fixture")
	dst := filepath.Join(t.TempDir(), "fixture")
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			if rel == ".weave" || rel == "node_modules" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// The gate must not pass the second time it sees the same broken edit: the last
// index has moved on by then, the commit has not.
func TestSecondCheckStillFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write("backend/src/tags.ts", "router.get('/tags', (req, res) => res.json([]))\n")
	write("frontend/src/api.ts", "export const tags = () => axios.get('/tags')\n")
	write(".gitignore", ".weave/\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "base")
	if _, err := weave.Reindex(root); err != nil {
		t.Fatal(err)
	}
	write("backend/src/tags.ts", "router.get('/tag-list', (req, res) => res.json([]))\n")
	for run := 1; run <= 2; run++ {
		g, err := weave.Reindex(root)
		if err != nil {
			t.Fatal(err)
		}
		res := Run(g, root, "", false, nil)
		if len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "GET /tags from frontend/src/api.ts:1") {
			t.Fatalf("run %d: errors = %v", run, res.Errors)
		}
	}
}
