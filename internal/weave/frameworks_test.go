package weave

import (
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/jsjson"
)

func src(rel, text string) SourceFile {
	return SourceFile{Rel: rel, Text: text, Lines: strings.Split(text, "\n")}
}

// Next publishes screens as file paths. A repo with 14 `page.tsx` files and no
// <Route> used to produce zero pages and answer nothing about any of them.
func TestExtractNextPages(t *testing.T) {
	files := []SourceFile{
		src("src/app/page.tsx", "export default function Home() {}\n"),
		src("src/app/(marketing)/pricing/page.tsx", "// Prices\nexport default function P() {}\n"),
		src("src/app/dashboard/page.tsx", "export default function D() {}\n"),
		src("src/app/api/quotes/route.ts", "export async function GET() {}\n"),
		src("pages/blog/[slug].tsx", "export default function B() {}\n"),
		src("pages/index.tsx", "export default function I() {}\n"),
		src("pages/_app.tsx", "export default function App() {}\n"),
		src("pages/api/health.ts", "export default function h() {}\n"),
	}
	b := NewBuilder()
	st := ExtractNextPages(files, b, true)

	got := map[string]bool{}
	for _, n := range b.nodes {
		if n.Kind == KindPage {
			got[n.Name] = true
		}
	}
	want := []string{"/", "/pricing", "/dashboard", "/blog/:slug"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing page %q; got %v", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("pages: got %d %v, want %d", len(got), got, len(want))
	}
	if st.Files != 5 || st.Found != 5 {
		t.Errorf("coverage: got %d/%d, want 5/5", st.Found, st.Files)
	}
}

// Without a next dependency, pages/x.tsx is a folder of React screens, not routes.
func TestExtractNextPagesWithoutNext(t *testing.T) {
	files := []SourceFile{src("src/pages/Orders.tsx", "export default function O() {}\n")}
	b := NewBuilder()
	if st := ExtractNextPages(files, b, false); st.Files != 0 || len(b.nodes) != 0 {
		t.Errorf("got %d pages from a non-Next pages/ dir, want none", len(b.nodes))
	}
}

// A service worker's fetch options span lines. Read as GET, the call matched no
// endpoint and `weave check` failed a file that was correct.
func TestFetchMethodOnNextLine(t *testing.T) {
	files := []SourceFile{
		src("public/sw.js", "return fetch(`/api/v1/tasks/${id}/push-action`, {\n  method: 'POST',\n  body: x,\n})\n"),
		src("src/a.ts", "fetch('/api/v1/things')\n"),
	}
	calls := ExtractCalls(files)
	if len(calls) != 2 {
		t.Fatalf("calls: got %d, want 2 (%v)", len(calls), calls)
	}
	if calls[0].Method != "POST" {
		t.Errorf("method: got %s, want POST", calls[0].Method)
	}
	if calls[1].Method != "GET" {
		t.Errorf("method without options: got %s, want GET", calls[1].Method)
	}
}

// A path whose first segment is a variable is unknowable, not broken.
func TestUnknowablePath(t *testing.T) {
	for p, want := range map[string]bool{
		"/${source}?search=x":          true,
		"${base}/x":                    true,
		"/reports${params}":            true,
		"/tasks${qs}":                  true,
		"/tasks?status=${s}":           false,
		"/api/v1/tasks?x=${y}":         false,
		"/api/v1/tasks/${id}/complete": false,
		"/api/v1/tasks/:id":            false,
		"/api/v1/tasks":                false,
	} {
		if got := unknowablePath(p); got != want {
			t.Errorf("%q: got %v, want %v", p, got, want)
		}
	}
}

// Repos commit specs as api/spec.json, docs/petstore.json, or YAML. Content decides
// what is a JSON spec; a YAML name decides a YAML one, and both are read now.
func TestExtractOpenAPIDetectsContentAndParsesYAML(t *testing.T) {
	spec := `{"openapi":"3.0.0","paths":{"/pets":{"get":{}},"/pets/{id}":{"delete":{}}}}`
	files := []SourceFile{
		src("api/spec.json", spec),
		src("docs/petstore.json", spec),
		src("docs/openapi.yaml", "openapi: 3.0.0\npaths:\n  /cats:\n    get: {}\n  /cats/{id}:\n    delete: {}\n"),
		src("package.json", `{"name":"x","paths":{},"scripts":{}}`),
	}
	b := NewBuilder()
	st := ExtractOpenAPI(files, b)

	if st.Files != 3 || st.Found != 3 {
		t.Errorf("specs: got %d/%d, want 3/3", st.Found, st.Files)
	}
	if len(st.Endpoints) != 6 {
		t.Errorf("endpoints: got %d, want 6", len(st.Endpoints))
	}
	if len(st.Ignored) != 0 {
		t.Errorf("ignored: got %v, want none", st.Ignored)
	}
	if len(st.Silent) != 0 {
		t.Errorf("silent: got %v, want none", st.Silent)
	}
}

// The same contract in JSON and YAML must produce the same graph, down to node
// metadata; YAML-only conveniences (quoted paths, anchors) must not change it.
func TestExtractOpenAPIYAMLParity(t *testing.T) {
	jsonSpec := `{"openapi":"3.0.0","paths":{"/pets":{"get":{"tags":["pets"],"summary":"List pets"}},"/pets/{id}":{"delete":{"tags":["pets"],"description":"Remove a pet"}}}}`
	yamlSpec := `openapi: "3.0.0"
x-shared: &shared
  tags: [pets]
  summary: List pets
paths:
  "/pets":
    get: *shared
  "/pets/{id}":
    delete:
      tags: [pets]
      description: Remove a pet
`
	run := func(rel, text string) (ExtractStats, *Builder) {
		b := NewBuilder()
		return ExtractOpenAPI([]SourceFile{src(rel, text)}, b), b
	}
	jst, jb := run("api/openapi.json", jsonSpec)
	yst, yb := run("api/openapi.yaml", yamlSpec)

	if jst.Files != 1 || yst.Files != 1 || jst.Found != 1 || yst.Found != 1 {
		t.Fatalf("coverage: json %d/%d, yaml %d/%d", jst.Found, jst.Files, yst.Found, yst.Files)
	}
	if len(jst.Endpoints) != len(yst.Endpoints) {
		t.Fatalf("endpoints: json %d, yaml %d", len(jst.Endpoints), len(yst.Endpoints))
	}
	for i := range jst.Endpoints {
		j, y := jst.Endpoints[i], yst.Endpoints[i]
		if j.Method != y.Method || j.Path != y.Path || j.Module != y.Module || j.ID != y.ID {
			t.Errorf("endpoint %d: json %+v, yaml %+v", i, j, y)
		}
	}
	if got, want := snapshotNodes(yb), snapshotNodes(jb); len(got) != len(want) {
		t.Fatalf("nodes: yaml %v, json %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("node %d: yaml %q, json %q", i, got[i], want[i])
			}
		}
	}
}

// The file path is necessarily different between the two runs; everything else
// a consumer sees must match.
func snapshotNodes(b *Builder) []string {
	out := make([]string, 0, len(b.nodes))
	for _, n := range b.nodes {
		line := n.ID + "|" + n.Kind + "|" + n.Name
		if n.Meta != nil {
			line += "|" + string(jsjson.Marshal(jsjson.Obj(n.Meta)))
		}
		out = append(out, line)
	}
	return out
}

// A malformed or unsupported YAML spec is a silent file, never an ignored one:
// "not read" would hide a contract the repo actually committed.
func TestExtractOpenAPIReportsInvalidYAMLAsSilent(t *testing.T) {
	files := []SourceFile{
		src("docs/openapi.yaml", "openapi: 3.0.0\npaths:\n  /x:\n   get: [\n"),
		src("docs/swagger.yaml", "swagger: \"2.0\"\npaths:\n  /v1/items:\n    get: {}\n"),
		src("api-spec.yml", "openapi: 3.0.0\npaths:\n  /y:\n    post: {}\n"),
		src("spec.yaml", "openapi: 3.0.0\npaths:\n  /z:\n    get: {}\n  /z:\n    post: {}\n"),
	}
	b := NewBuilder()
	st := ExtractOpenAPI(files, b)

	if st.Files != 4 || st.Found != 2 {
		t.Errorf("specs: got %d/%d, want 2/4", st.Found, st.Files)
	}
	if len(st.Endpoints) != 2 {
		t.Errorf("endpoints: got %d (%v), want 2", len(st.Endpoints), st.Endpoints)
	}
	wantSilent := map[string]bool{"docs/openapi.yaml": true, "spec.yaml": true}
	if len(st.Silent) != len(wantSilent) {
		t.Fatalf("silent: got %v, want %v", st.Silent, wantSilent)
	}
	for _, f := range st.Silent {
		if !wantSilent[f] {
			t.Errorf("silent: unexpected %q in %v", f, st.Silent)
		}
	}
	if len(st.Ignored) != 0 {
		t.Errorf("ignored: got %v, want none", st.Ignored)
	}
}

// spec.yaml is a common name for other tools' files. Valid YAML that does not
// declare OpenAPI must not be reported as an OpenAPI gap, even under a name that
// looks like a spec.
func TestExtractOpenAPISkipsUnrelatedYAML(t *testing.T) {
	files := []SourceFile{
		src("spec.yaml", "name: build\nsteps:\n  - run: make\n"),
		src("api-spec.yml", "name: deploy\nsteps: [test, ship]\n"),
		src("docs/openapi.yaml", "service: worker\nqueue: jobs\n"),
		src("spec.yml", "paths: [\n"), // malformed, but makes no OpenAPI claim
	}
	b := NewBuilder()
	st := ExtractOpenAPI(files, b)

	if st.Files != 0 || st.Found != 0 {
		t.Errorf("specs: got %d/%d, want none", st.Found, st.Files)
	}
	if len(st.Endpoints) != 0 || len(b.nodes) != 0 {
		t.Errorf("extracted %d endpoints and %d nodes from unrelated YAML", len(st.Endpoints), len(b.nodes))
	}
	if len(st.Silent) != 0 || len(st.Ignored) != 0 {
		t.Errorf("silent/ignored: got %v/%v, want none", st.Silent, st.Ignored)
	}
}

// A generic name plus an OpenAPI marker is still a spec, so a broken one keeps
// the useful report a clearly named openapi.yaml gets.
func TestExtractOpenAPIReportsMalformedMarkedYAML(t *testing.T) {
	files := []SourceFile{
		src("spec.yaml", "openapi: 3.0.0\npaths:\n  /x:\n   get: [\n"),
		src("api-spec.yml", "swagger: \"2.0\"\npaths:\n  /y:\n   get: {\n"),
	}
	b := NewBuilder()
	st := ExtractOpenAPI(files, b)

	if st.Files != 2 || st.Found != 0 {
		t.Errorf("specs: got %d/%d, want 0/2", st.Found, st.Files)
	}
	if len(st.Silent) != 2 {
		t.Errorf("silent: got %v, want both files", st.Silent)
	}
	if len(st.Ignored) != 0 {
		t.Errorf("ignored: got %v, want none", st.Ignored)
	}
}

// Shared YAML anchors are common in hand-written specs; the summary they carry
// must reach the graph.
func TestExtractOpenAPIYAMLAnchors(t *testing.T) {
	yamlSpec := `openapi: 3.0.0
x-shared: &shared
  tags: [pets]
  summary: Fetch a pet
paths:
  "/pets/{id}":
    get: *shared
`
	b := NewBuilder()
	st := ExtractOpenAPI([]SourceFile{src("docs/openapi.yaml", yamlSpec)}, b)
	if st.Files != 1 || st.Found != 1 || len(st.Endpoints) != 1 {
		t.Fatalf("specs: got %d/%d, %d endpoints", st.Found, st.Files, len(st.Endpoints))
	}
	e := st.Endpoints[0]
	if e.Method != "GET" || e.Path != "/pets/:id" || e.Module != "pets" {
		t.Errorf("endpoint: got %+v, want GET /pets/:id in pets", e)
	}
	for _, n := range b.nodes {
		if n.ID != e.ID {
			continue
		}
		desc, _ := n.Meta.Get("desc")
		if desc.Str() != "Fetch a pet" {
			t.Errorf("desc: got %q, want %q", desc.Str(), "Fetch a pet")
		}
		return
	}
	t.Errorf("endpoint node %q missing", e.ID)
}
