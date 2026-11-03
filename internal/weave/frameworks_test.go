package weave

import (
	"strings"
	"testing"
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
// what is a spec; YAML is named as a gap instead of being silently skipped.
func TestExtractOpenAPIDetectsContentAndReportsYAML(t *testing.T) {
	spec := `{"openapi":"3.0.0","paths":{"/pets":{"get":{}},"/pets/{id}":{"delete":{}}}}`
	files := []SourceFile{
		src("api/spec.json", spec),
		src("docs/petstore.json", spec),
		src("docs/openapi.yaml", "openapi: 3.0.0\npaths:\n  /cats:\n    get: {}\n"),
		src("package.json", `{"name":"x","paths":{},"scripts":{}}`),
	}
	b := NewBuilder()
	st := ExtractOpenAPI(files, b)

	if st.Files != 2 || st.Found != 2 {
		t.Errorf("specs: got %d/%d, want 2/2", st.Found, st.Files)
	}
	if len(st.Endpoints) != 4 {
		t.Errorf("endpoints: got %d, want 4", len(st.Endpoints))
	}
	if len(st.Ignored) != 1 || st.Ignored[0] != "docs/openapi.yaml" {
		t.Errorf("ignored: got %v, want [docs/openapi.yaml]", st.Ignored)
	}
}
