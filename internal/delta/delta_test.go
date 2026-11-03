package delta

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/weave"
)

func copyFixture(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "test", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	err = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func saveBase(t *testing.T, g *weave.Graph) *weave.Graph {
	t.Helper()
	p := filepath.Join(t.TempDir(), "base-graph.json")
	if err := os.WriteFile(p, g.MarshalJS(), 0o644); err != nil {
		t.Fatal(err)
	}
	base := baseline.GraphFromFile(p)
	if base == nil {
		t.Fatal("GraphFromFile returned nil for a graph this test just wrote")
	}
	return base
}

func rewrite(t *testing.T, path string, edits ...string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for i := 0; i+1 < len(edits); i += 2 {
		text = strings.ReplaceAll(text, edits[i], edits[i+1])
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeltaRouteRenameMatchesTheTypeScript(t *testing.T) {
	root := copyFixture(t)
	before := weave.BuildGraph(root)
	base := saveBase(t, before)

	empty := "delta vs base-graph.json\n" +
		"no change in endpoints, pages, tables, modules, requirements or changes\n" +
		"0 added · 0 removed · 0 moved"
	if got := Delta(before, base, "base-graph.json", false); got != empty {
		t.Errorf("unchanged delta:\n--- got ---\n%s\n--- want ---\n%s", got, empty)
	}

	rewrite(t, filepath.Join(root, "packages", "backend", "src", "modules", "orders", "orders.routes.ts"),
		"router.get('/',", "router.get('/v2',",
		"router.post('/',", "router.post('/v2',")
	after := weave.BuildGraph(root)

	want := `delta vs base-graph.json
added 2
+ endpoint  GET /api/v1/orders/v2  packages/backend/src/modules/orders/orders.routes.ts:6
+ endpoint  POST /api/v1/orders/v2  packages/backend/src/modules/orders/orders.routes.ts:9
removed 2
- endpoint  GET /api/v1/orders  packages/backend/src/modules/orders/orders.routes.ts:6  still called by 1 in the base
- endpoint  POST /api/v1/orders  packages/backend/src/modules/orders/orders.routes.ts:9
2 added · 2 removed · 0 moved`
	if got := Delta(after, base, "base-graph.json", false); got != want {
		t.Errorf("renamed route:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestDeltaMovedMatchesTheTypeScript(t *testing.T) {
	root := copyFixture(t)
	before := weave.BuildGraph(root)
	base := saveBase(t, before)

	moved := filepath.Join(root, "packages", "web", "src", "moved")
	if err := os.MkdirAll(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(
		filepath.Join(root, "packages", "web", "src", "pages", "Orders.tsx"),
		filepath.Join(moved, "Orders.tsx"),
	); err != nil {
		t.Fatal(err)
	}
	rewrite(t, filepath.Join(root, "packages", "web", "src", "App.tsx"), "./pages/Orders", "./moved/Orders")
	after := weave.BuildGraph(root)

	want := `delta vs base-graph.json
moved 1
~ page      /orders  packages/web/src/pages/Orders.tsx → packages/web/src/moved/Orders.tsx
0 added · 0 removed · 1 moved`
	if got := Delta(after, base, "base-graph.json", false); got != want {
		t.Errorf("moved page:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestDeltaKindsCapsAndAll(t *testing.T) {
	base := &weave.Graph{}
	for _, k := range kinds {
		base.Nodes = append(base.Nodes, n(k, "removed "+k, "old:"+k, "a.ts", 1))
	}
	base.Nodes = append(base.Nodes,
		n(weave.KindComponent, "ignored", "old:comp", "a.ts", 1),
		n(weave.KindCall, "ignored call", "old:call", "a.ts", 1),
	)
	current := &weave.Graph{}
	for i := 1; i <= 13; i++ {
		name := fmt.Sprintf("GET /n%02d", i)
		current.Nodes = append(current.Nodes, n(weave.KindEndpoint, name, "new:"+name, "a.ts", 0))
	}

	want := `delta vs synthetic
added 13
+ endpoint  GET /n01  a.ts
+ endpoint  GET /n02  a.ts
+ endpoint  GET /n03  a.ts
+ endpoint  GET /n04  a.ts
+ endpoint  GET /n05  a.ts
+ endpoint  GET /n06  a.ts
+ endpoint  GET /n07  a.ts
+ endpoint  GET /n08  a.ts
+ endpoint  GET /n09  a.ts
+ endpoint  GET /n10  a.ts
+ endpoint  GET /n11  a.ts
+ endpoint  GET /n12  a.ts
  +1 more (--all)
removed 6
- endpoint  removed endpoint  a.ts:1
- page      removed page  a.ts:1
- table     removed table  a.ts:1
- module    removed module  a.ts:1
- change    removed change  a.ts:1
- req       removed req  a.ts:1
13 added · 6 removed · 0 moved`
	if got := Delta(current, base, "synthetic", false); got != want {
		t.Errorf("kinds and caps:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	all := Delta(current, base, "synthetic", true)
	if strings.Contains(all, "more (--all)") {
		t.Errorf("--all still capped:\n%s", all)
	}
	if got := strings.Count(all, "\n+ endpoint  "); got != 13 {
		t.Errorf("--all showed %d added lines, want 13", got)
	}
}

func n(kind, name, id, file string, line int) *weave.GNode {
	return &weave.GNode{ID: id, Kind: kind, Name: name, File: weave.Ptr(file), Line: weave.Ptr(line)}
}

// The explorer's "Since the last commit" reads the same diff as `weave delta`:
// added, removed with the base's file call sites, and moved with the old file.
func TestExploreDeltaCarriesCallersAndOldFile(t *testing.T) {
	str := func(s string) *string { return &s }
	base := &weave.Graph{
		Nodes: []*weave.GNode{
			{ID: "endpoint:GET /a", Kind: weave.KindEndpoint, Name: "GET /a", File: str("a.ts")},
			{ID: "page:/p", Kind: weave.KindPage, Name: "/p", File: str("old.tsx")},
			{ID: "file:web/x.ts", Kind: weave.KindFile, Name: "web/x.ts", File: str("web/x.ts")},
		},
		Edges: []weave.GEdge{{From: "file:web/x.ts", To: "endpoint:GET /a", Kind: "calls", Conf: weave.ConfExact}},
	}
	current := &weave.Graph{
		Nodes: []*weave.GNode{
			{ID: "endpoint:GET /b", Kind: weave.KindEndpoint, Name: "GET /b", File: str("b.ts")},
			{ID: "page:/p", Kind: weave.KindPage, Name: "/p", File: str("new.tsx")},
		},
	}
	d := Explore(current, base, "HEAD")
	if d.Ref != "HEAD" || len(d.Added) != 1 || len(d.Removed) != 1 || len(d.Moved) != 1 {
		t.Fatalf("delta = %+v", d)
	}
	if d.Added[0].ID != "endpoint:GET /b" {
		t.Errorf("added = %q", d.Added[0].ID)
	}
	if d.Removed[0].ID != "endpoint:GET /a" || d.Removed[0].Callers != 1 {
		t.Errorf("removed = %+v, want GET /a still called by 1", d.Removed[0])
	}
	if d.Moved[0].From == nil || *d.Moved[0].From != "old.tsx" || *d.Moved[0].File != "new.tsx" {
		t.Errorf("moved = %+v", d.Moved[0])
	}
}
