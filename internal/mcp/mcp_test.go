package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

func TestMain(m *testing.M) {
	// Tool calls record usage; tests must not write a ledger into the fixture.
	_ = os.Setenv("WEAVE_NO_USAGE", "1")
	home, err := os.MkdirTemp("", "weave-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("WEAVE_HOME", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

func replay(t *testing.T, flags string, src GraphSource, input string) ([]map[string]any, []string) {
	t.Helper()
	return replayWith(t, flags, src, input, nil)
}

func replayWith(t *testing.T, flags string, src GraphSource, input string, invalidate func()) ([]map[string]any, []string) {
	t.Helper()
	var out bytes.Buffer
	multi := strings.Contains(flags, "all")
	manager := strings.Contains(flags, "manager")
	if err := ServeIO(strings.NewReader(input), &out, src, multi, manager, invalidate); err != nil {
		t.Fatal(err)
	}
	var msgs []map[string]any
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		lines = append(lines, line)
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad json line %q: %v", line, err)
		}
		msgs = append(msgs, m)
	}
	return msgs, lines
}

func encodeNoHTML(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

func TestToolListAndInitializeMatchTypeScript(t *testing.T) {
	for _, tc := range []struct{ name, flags string }{
		{"default", ""},
		{"all", "all"},
		{"manager", "manager"},
		{"manager-all", "manager all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, raw := replay(t, tc.flags, func(string) (*weave.Graph, error) { return &weave.Graph{}, nil }, strings.Join([]string{
				`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
				`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			}, "\n")+"\n")
			if len(raw) != 2 {
				t.Fatalf("got %d replies", len(raw))
			}
			for _, pair := range []struct {
				id   int
				file string
			}{{1, "init-" + tc.name + ".json"}, {2, "tools-" + tc.name + ".json"}} {
				want, err := os.ReadFile(filepath.Join("testdata", pair.file))
				if err != nil {
					t.Fatal(err)
				}
				var env struct {
					Result json.RawMessage `json:"result"`
				}
				if err := json.Unmarshal([]byte(raw[pair.id-1]), &env); err != nil {
					t.Fatal(err)
				}
				var got, wantAny any
				if err := json.Unmarshal(env.Result, &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(want, &wantAny); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, wantAny) {
					t.Fatalf("%s differs from the TypeScript golden:\n got: %s\nwant: %s", pair.file, encodeNoHTML(got), strings.TrimSpace(string(want)))
				}
			}
		})
	}
}

func TestContextCallErrorsAndReceipt(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	src := func(string) (*weave.Graph, error) { return g, nil }
	msgs, _ := replay(t, "", src, strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context","arguments":{"files":["packages/backend/src/modules/orders/orders.routes.ts"]}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"nope"}`,
	}, "\n")+"\n")

	text := msgs[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "GET /api/v1/orders") {
		t.Fatalf("context pack missing the endpoint: %s", text)
	}
	if !regexp.MustCompile(`\x{2014} \d+ tokens · graph \d+s old · coverage \d+/\d+ route files`).MatchString(text) {
		t.Fatalf("receipt missing: %s", text)
	}
	toolErr := msgs[1]["error"].(map[string]any)
	if toolErr["code"].(float64) != -32602 || !strings.Contains(toolErr["message"].(string), "unknown tool nope; available: context, impact") {
		t.Fatalf("unknown tool shape: %v", toolErr)
	}
	methodErr := msgs[2]["error"].(map[string]any)
	if methodErr["code"].(float64) != -32601 || methodErr["message"].(string) != "method not found: nope" {
		t.Fatalf("unknown method shape: %v", methodErr)
	}
}

func TestMalformedRequestsKeepTheirShapes(t *testing.T) {
	msgs, _ := replay(t, "", func(string) (*weave.Graph, error) { return &weave.Graph{}, nil }, strings.Join([]string{
		`{"jsonrpc":"2.0","id":1}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{}}`,
	}, "\n")+"\n")
	if got := msgs[0]["error"].(map[string]any)["message"]; got != "method not found: undefined" {
		t.Fatalf("missing method: %v", got)
	}
	got := msgs[1]["error"].(map[string]any)["message"].(string)
	if !strings.HasPrefix(got, "unknown tool undefined; available: context, impact") {
		t.Fatalf("missing tool name: %s", got)
	}
}

func TestNotesAndChangedOverMCP(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	src := func(string) (*weave.Graph, error) { return g, nil }
	base := filepath.Join(t.TempDir(), "base.json")
	if err := os.WriteFile(base, []byte(g.MarshalJS()), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs, _ := replay(t, "", src, strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"remember","arguments":{"files":["packages/backend/src/modules/orders/orders.routes.ts"],"note":"go port note"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"recall","arguments":{"files":["packages/backend/src/modules/orders/orders.routes.ts"]}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"changed","arguments":{"from":"` + strings.ReplaceAll(base, `\`, `\\`) + `"}}}`,
	}, "\n")+"\n")
	t.Cleanup(func() {
		_ = os.Remove(weave.MemoryPath(root))
		_ = os.Remove(filepath.Join(root, ".weave"))
	})
	recall := msgs[1]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(recall, "go port note") {
		t.Fatalf("recall did not return the note: %s", recall)
	}
	changed := msgs[2]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(changed, "no change in endpoints") {
		t.Fatalf("changed against the same graph: %s", changed)
	}
}

// An agent closes a task over MCP, and the server's cached graph is invalidated so
// the next plan() does not repeat the same task.
func TestTaskDoneAndReopenOverMCP(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(path, []byte("# Tasks: a\n\n- [ ] T1 One.\n- [x] T2 Two.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := func(string) (*weave.Graph, error) { return &weave.Graph{Root: root}, nil }
	invalidated := 0
	msgs, _ := replayWith(t, "", src, strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"task_done","arguments":{"change":"2026-01-01-a","task":"T1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"task_reopen","arguments":{"change":"2026-01-01-a","task":"T2"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"task_done","arguments":{"change":"../x","task":"T1"}}}`,
	}, "\n")+"\n", func() { invalidated++ })

	text := func(i int) string {
		return msgs[i]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	}
	if got := text(0); !strings.HasPrefix(got, "done 2026-01-01-a T1") {
		t.Errorf("task_done reply: %s", got)
	}
	if got := text(1); !strings.HasPrefix(got, "reopened 2026-01-01-a T2") {
		t.Errorf("task_reopen reply: %s", got)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "- [x] T1 One.") || !strings.Contains(string(raw), "- [ ] T2 Two.") {
		t.Errorf("tasks.md after both edits:\n%s", raw)
	}
	if invalidated != 2 {
		t.Errorf("graph invalidations: %d, want 2", invalidated)
	}
	if _, ok := msgs[2]["error"]; !ok {
		t.Errorf("a path outside specs/changes should be an error, got %v", msgs[2])
	}
}

// verify is the gate over MCP: the lines `weave check` prints, then the receipt.
func TestVerifyOverMCP(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	g := weave.BuildGraph(root)
	src := func(string) (*weave.Graph, error) { return g, nil }
	msgs, _ := replay(t, "", src, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"verify","arguments":{"files":["packages/backend/src/modules/orders/orders.routes.ts"]}}}`+"\n")

	text := msgs[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.HasPrefix(text, "check: 1 file(s)") || !strings.Contains(text, "2 endpoint(s)") {
		t.Fatalf("verify reply: %s", text)
	}
	if !strings.Contains(text, "  run  cd packages/web") {
		t.Fatalf("verify did not name the command: %s", text)
	}
	if !regexp.MustCompile(`\x{2014} \d+ tokens · graph \d+s old`).MatchString(text) {
		t.Fatalf("receipt missing: %s", text)
	}
}

// A change is created and removed over MCP; the destructive tool needs confirm, and
// both writes drop the cached graph.
func TestChangeNewAndDeleteOverMCP(t *testing.T) {
	root := t.TempDir()
	src := func(string) (*weave.Graph, error) { return &weave.Graph{Root: root}, nil }
	invalidated := 0
	mark := func() { invalidated++ }
	msgs, _ := replayWith(t, "", src, strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"change_new","arguments":{"name":"2026-01-01-billing","title":"Billing"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"change_new","arguments":{"name":"../escape"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"change_delete","arguments":{"change":"2026-01-01-billing"}}}`,
	}, "\n")+"\n", mark)

	text := func(i int) string {
		return msgs[i]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	}
	if got := text(0); !strings.HasPrefix(got, "created specs/changes/2026-01-01-billing") {
		t.Errorf("change_new reply: %s", got)
	}
	dir := filepath.Join(root, "specs", "changes", "2026-01-01-billing")
	if _, err := os.Stat(filepath.Join(dir, "tasks.md")); err != nil {
		t.Errorf("tasks.md was not created: %v", err)
	}
	if _, ok := msgs[1]["error"]; !ok {
		t.Error("a name with a separator should be an error")
	}
	if _, ok := msgs[2]["error"]; !ok {
		t.Error("delete without confirm should be an error")
	}

	msgs, _ = replayWith(t, "", src, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"change_delete","arguments":{"change":"2026-01-01-billing","confirm":true}}}`+"\n", mark)
	if got := text(0); !strings.HasPrefix(got, "deleted specs/changes/2026-01-01-billing") {
		t.Errorf("change_delete reply: %s", got)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the change directory survived")
	}
	if invalidated != 2 {
		t.Errorf("graph invalidations: %d, want 2", invalidated)
	}
}
