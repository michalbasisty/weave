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
	os.Exit(m.Run())
}

func replay(t *testing.T, flags string, src GraphSource, input string) ([]map[string]any, []string) {
	t.Helper()
	var out bytes.Buffer
	multi := strings.Contains(flags, "all")
	manager := strings.Contains(flags, "manager")
	if err := ServeIO(strings.NewReader(input), &out, src, multi, manager); err != nil {
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
