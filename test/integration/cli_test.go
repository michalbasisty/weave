package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The suite builds the real binary once and drives it exactly as a user would.
// No Node, no TypeScript: this is the durable contract after the Go cutover.

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "weave-bin-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	bin = filepath.Join(dir, "weave.exe")
	if os.PathSeparator != '\\' {
		bin = filepath.Join(dir, "weave")
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "./cmd/weave")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("build: " + err.Error() + "\n" + string(out))
	}
	os.Exit(m.Run())
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
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
}

func fixture(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "fixture")
	copyTree(t, filepath.Join("..", "fixture"), dst)
	return dst
}

func run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	if code != 0 && errb.Len() > 0 {
		t.Logf("stderr: %s", errb.String())
	}
	return out.String(), code
}

func TestIndexContextAndSearch(t *testing.T) {
	root := fixture(t)
	out, code := run(t, "index", "--root", root)
	if code != 0 || !strings.Contains(out, "indexed ") {
		t.Fatalf("index: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, ".weave", "graph.json")); err != nil {
		t.Fatal(err)
	}

	out, code = run(t, "context", "packages/backend/src/modules/orders/orders.routes.ts", "--budget", "900", "--root", root)
	if code != 0 {
		t.Fatalf("context: %s", out)
	}
	for _, want := range []string{"GET /api/v1/orders", "orders.routes.ts", "npx vitest run"} {
		if !strings.Contains(out, want) {
			t.Fatalf("context missing %q:\n%s", want, out)
		}
	}

	out, code = run(t, "context", "packages/backend/src/modules/orders/orders.routes.ts", "--depth", "code", "--budget", "900", "--root", root)
	if code != 0 || !strings.Contains(out, "router.get('/") {
		t.Fatalf("code depth: %s", out)
	}

	out, _ = run(t, "search", "orders", "--root", root)
	if !strings.HasPrefix(strings.TrimSpace(out), "module") && !strings.Contains(strings.Split(out, "\n")[0], "orders") {
		t.Fatalf("search: %s", out)
	}

	out, _ = run(t, "impact", "packages/backend/src/modules/orders/orders.routes.ts", "--root", root)
	for _, want := range []string{"tables", "orders", "pages"} {
		if !strings.Contains(out, want) {
			t.Fatalf("impact missing %q:\n%s", want, out)
		}
	}
}

func TestNotesAndGrep(t *testing.T) {
	root := fixture(t)
	file := "packages/backend/src/modules/orders/orders.routes.ts"
	if out, code := run(t, "remember", "--note", "orders share the prisma client", file, "--root", root); code != 0 {
		t.Fatalf("remember: %s", out)
	}
	out, _ := run(t, "context", file, "--budget", "900", "--root", root)
	if !strings.Contains(out, "orders share the prisma client") || !strings.Contains(out, "not evidence") {
		t.Fatalf("context did not carry the note:\n%s", out)
	}
	out, _ = run(t, "recall", file, "--root", root)
	if !strings.Contains(out, "orders share the prisma client") {
		t.Fatalf("recall: %s", out)
	}
	out, _ = run(t, "grep", "prisma.order", "--limit", "2", "--root", root)
	if !strings.Contains(out, "orders.routes.ts:") {
		t.Fatalf("grep: %s", out)
	}
}

func TestCheckFailsOnRenamedRoute(t *testing.T) {
	root := fixture(t)
	file := filepath.Join(root, "packages/backend/src/modules/orders/orders.routes.ts")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, "index", "--root", root); code != 0 {
		t.Fatalf("index: %s", out)
	}
	if err := os.WriteFile(file, []byte(strings.Replace(string(original), "router.get('/'", "router.get('/list'", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(file, original, 0o644)

	out, code := run(t, "check", "packages/backend/src/modules/orders/orders.routes.ts", "--root", root)
	if code != 1 {
		t.Fatalf("check should exit 1, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "removed endpoint still called") {
		t.Fatalf("check did not name the removed endpoint:\n%s", out)
	}
}

// runBoth captures stdout and stderr together: a hook writes its failure to stderr,
// which is exactly what the agent reads back.
func runBoth(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return string(out), code
}

// The hook is the in-session gate: a broken contract must exit 2 with the failure on
// stderr (what a Claude Code Stop hook reads back), and every run must leave a fire.
func TestHookFailsOnABrokenContractAndRecordsIt(t *testing.T) {
	root := fixture(t)
	file := filepath.Join(root, "packages/backend/src/modules/orders/orders.routes.ts")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, "index", "--root", root); code != 0 {
		t.Fatalf("index: %s", out)
	}

	if out, code := runBoth(t, "hook", "--root", root); code != 0 {
		t.Fatalf("hook on a clean tree should exit 0, got %d:\n%s", code, out)
	}

	if err := os.WriteFile(file, []byte(strings.Replace(string(original), "router.get('/'", "router.get('/list'", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(file, original, 0o644)

	out, code := runBoth(t, "hook", "--root", root)
	if code != 2 {
		t.Fatalf("hook should exit 2 for a Stop hook to block, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "removed endpoint still called") {
		t.Fatalf("hook did not name the removed endpoint:\n%s", out)
	}

	fires := filepath.Join(root, ".weave", "fires.jsonl")
	raw, err := os.ReadFile(fires)
	if err != nil {
		t.Fatalf("no fire recorded: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 fires, got %d:\n%s", len(lines), raw)
	}
	if !strings.Contains(lines[1], `"result":"fail"`) {
		t.Fatalf("second fire is not a failure: %s", lines[1])
	}

	report, rcode := run(t, "fires", "--root", root)
	if rcode != 0 || !strings.Contains(report, "1 failure") {
		t.Fatalf("fires should report one failure (code %d):\n%s", rcode, report)
	}
}

func TestDeltaAgainstSavedGraph(t *testing.T) {
	root := fixture(t)
	file := filepath.Join(root, "packages/backend/src/modules/orders/orders.routes.ts")
	if out, code := run(t, "index", "--root", root); code != 0 {
		t.Fatalf("index: %s", out)
	}
	base := filepath.Join(root, "before.json")
	graph, err := os.ReadFile(filepath.Join(root, ".weave", "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, graph, 0o644); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(file)
	if err := os.WriteFile(file, []byte(strings.Replace(string(original), "router.get('/'", "router.get('/list'", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.WriteFile(file, original, 0o644)

	out, code := run(t, "delta", "--from", base, "--root", root)
	if code != 0 {
		t.Fatalf("delta: %s", out)
	}
	for _, want := range []string{"+ endpoint", "GET /api/v1/orders/list", "- endpoint", "GET /api/v1/orders", "still called by 1 in the base"} {
		if !strings.Contains(out, want) {
			t.Fatalf("delta missing %q:\n%s", want, out)
		}
	}
}

func TestExploreWritesAPage(t *testing.T) {
	root := fixture(t)
	outPath := filepath.Join(root, "explore.html")
	out, code := run(t, "explore", "--no-open", "--out", outPath, "--root", root)
	if code != 0 || !strings.Contains(out, "explore:") {
		t.Fatalf("explore: %s", out)
	}
	html, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	for _, want := range []string{"<!doctype html>", "page:/orders", "weave"} {
		if !strings.Contains(s, want) {
			t.Fatalf("explore page missing %q", want)
		}
	}
	if strings.Contains(s, "undefined") {
		t.Fatal("explore page contains undefined")
	}
}

func TestMCPAndGain(t *testing.T) {
	root := fixture(t)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"context","arguments":{"files":["packages/backend/src/modules/orders/orders.routes.ts"]}}}`,
	}, "\n") + "\n"
	cmd := exec.Command(bin, "mcp", "--root", root)
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("mcp: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("mcp replies: %d\n%s", len(lines), out.String())
	}
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	json.Unmarshal([]byte(lines[1]), &list)
	names := make([]string, 0, len(list.Result.Tools))
	for _, tl := range list.Result.Tools {
		names = append(names, tl.Name)
	}
	if strings.Join(names, ",") != "context,impact,search,remember,recall,plan,ask,changes,changed,status" {
		t.Fatalf("tools: %v", names)
	}
	if !strings.Contains(lines[2], "GET /api/v1/orders") || !strings.Contains(lines[2], "tokens") {
		t.Fatalf("context reply: %s", lines[2])
	}
	gainOut, _ := run(t, "gain", "--since", "1", "--root", root)
	if !strings.Contains(gainOut, "call(s)") || !strings.Contains(gainOut, "tokens") {
		t.Fatalf("gain: %s", gainOut)
	}
}

func TestCheckFailsWhenRequirementTestFails(t *testing.T) {
	root := fixture(t)
	if out, code := run(t, "index", "--root", root); code != 0 {
		t.Fatalf("index: %s", out)
	}
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	junitXML := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites><testsuite name="orders">
  <testcase classname="orders" name="REQ-ORD-1 orders page loads">
    <failure message="boom">expected 1, got 0</failure>
  </testcase>
</testsuite></testsuites>`
	if err := os.WriteFile(filepath.Join(root, ".weave", "junit.xml"), []byte(junitXML), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, "check", "packages/backend/src/modules/orders/orders.routes.ts", "--root", root)
	if code != 1 {
		t.Fatalf("check should fail on the requirement test, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "requirement test failing") || !strings.Contains(out, "REQ-ORD-1") {
		t.Fatalf("check did not name the failing requirement:\n%s", out)
	}
	ev, _ := run(t, "evidence", "--root", root)
	if !strings.Contains(ev, "REQ-ORD-1") || !strings.Contains(ev, "FAIL") {
		t.Fatalf("evidence:\n%s", ev)
	}
}

func TestInitUpgradeReplacesStaleEntry(t *testing.T) {
	stale := `{"mcpServers":{"weave":{"command":"node","args":["C:/old/src/cli.ts","mcp"]}}}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, "init", "--upgrade", "--root", dir)
	if code != 0 || !strings.Contains(out, ".mcp.json (upgraded)") {
		t.Fatalf("init --upgrade: %d\n%s", code, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.MCPServers["weave"].Command; got == "" {
		t.Fatalf("no command in %s", raw)
	} else if gotInfo, err := os.Stat(got); err != nil {
		t.Fatalf("command %q does not exist: %v", got, err)
	} else if binInfo, err := os.Stat(bin); err != nil {
		t.Fatalf("test binary: %v", err)
	} else if !os.SameFile(gotInfo, binInfo) {
		t.Fatalf("command = %q, want the test binary %q", got, bin)
	}

	// Without the flag an existing file is untouched.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, ".mcp.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := run(t, "init", "--root", dir2); code != 0 {
		t.Fatal("init failed")
	}
	raw2, _ := os.ReadFile(filepath.Join(dir2, ".mcp.json"))
	if !strings.Contains(string(raw2), `"node"`) {
		t.Fatalf("init without --upgrade rewrote the file: %s", raw2)
	}
}

func TestSpecNewAndTaskDone(t *testing.T) {
	root := t.TempDir()
	out, code := run(t, "spec", "new", "demo", "--title", "Demo change", "--root", root)
	if code != 0 || !strings.Contains(out, "created specs/changes/") {
		t.Fatalf("spec new: %d\n%s", code, out)
	}
	matches, err := filepath.Glob(filepath.Join(root, "specs", "changes", "*-demo", "tasks.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("tasks.md: %v %v", matches, err)
	}
	raw, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(raw), "- [ ] T1") {
		t.Fatalf("template: %s", raw)
	}
	slug := filepath.Base(filepath.Dir(matches[0]))
	out, code = run(t, "spec", "task", "done", slug, "T1", "--root", root)
	if code != 0 || !strings.Contains(out, "done "+slug+" T1") {
		t.Fatalf("task done: %d\n%s", code, out)
	}
	raw, _ = os.ReadFile(matches[0])
	if !strings.Contains(string(raw), "- [x] T1") {
		t.Fatalf("task not ticked: %s", raw)
	}
}

func TestVersion(t *testing.T) {
	out, code := run(t, "version")
	if code != 0 || !strings.HasPrefix(strings.TrimSpace(out), "weave ") {
		t.Fatalf("version: %d %q", code, out)
	}
}

func TestInitWritesTheBinaryPath(t *testing.T) {
	dir := t.TempDir()
	out, code := run(t, "init", "--root", dir)
	if code != 0 || !strings.Contains(out, "weave init in") {
		t.Fatalf("init: %s", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	w, ok := cfg.MCPServers["weave"]
	if !ok || w.Command == "" || !filepath.IsAbs(w.Command) {
		t.Fatalf("mcp config: %s", raw)
	}
	if strings.Join(w.Args, " ") != "mcp --root ." {
		t.Fatalf("args: %v", w.Args)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), ".weave/") {
		t.Fatalf("gitignore: %s", gi)
	}
}
