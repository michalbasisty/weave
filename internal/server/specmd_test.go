package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

// The spec renderer reads the shapes the capability specs use, and an HTML tag in a
// spec stays text.
func TestRenderSpecMD(t *testing.T) {
	md := "# Capability: media-files\n\n" +
		"- **Status**: `reconstructed` — kept\n  on one line.\n" +
		"- **Requirements**: `REQ-M-1` to `REQ-M-2`.\n\n" +
		"## Parts\n\n" +
		"| Path | What it does |\n| ---- | ------------ |\n" +
		"| `src/a.ts` | <script>alert(1)</script> serves. |\n\n" +
		"A paragraph\nthat wraps.\n\n" +
		"1. first\n2. second\n"
	got := renderSpecMD(md)

	for _, want := range []string{
		"<h1>Capability: media-files</h1>",
		"<ul><li><strong>Status</strong>: <code>reconstructed</code> — kept on one line.</li><li><strong>Requirements</strong>: <code>REQ-M-1</code> to <code>REQ-M-2</code>.</li></ul>",
		`<div class="mdtable"><table><thead><tr><th>Path</th><th>What it does</th></tr></thead><tbody><tr><td><code>src/a.ts</code></td><td>&lt;script&gt;alert(1)&lt;/script&gt; serves.</td></tr></tbody></table></div>`,
		"<p>A paragraph that wraps.</p>",
		"<ol><li>first</li><li>second</li></ol>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<script>") {
		t.Errorf("a spec can inject HTML: %s", got)
	}
	if title := specTitle(md); title != "Capability: media-files" {
		t.Errorf("title = %q", title)
	}
}

func TestSpecPageRenders(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "capabilities", "media-files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("# Capability: media-files\n\n## Parts\n\n| Path | What it does |\n| ---- | --- |\n| `src/a.ts` | Serves. |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return &weave.Graph{}, nil }})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/spec?path=specs/capabilities/media-files/spec.md"))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct{ Path, Title, HTML string }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Capability: media-files" || !strings.Contains(got.HTML, "src/a.ts") || got.Path != "specs/capabilities/media-files/spec.md" {
		t.Errorf("unexpected page: %+v", got)
	}
	for _, bad := range []string{
		"specs/../a.md",
		"src/a.ts",
		"specs/capabilities/media-files/spec.txt",
		"specs/capabilities/nope/spec.md",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, get("/api/spec?path="+url.QueryEscape(bad)))
		if rec.Code != 400 {
			t.Errorf("%s: got %d, want 400", bad, rec.Code)
		}
	}
}
