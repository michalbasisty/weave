package server

import (
	"bytes"
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

// The editor's save goes through the panel: token, path guard, size cap, and the
// file and rendered page both show the new text.
func TestSpecSaveWritesThroughThePanel(t *testing.T) {
	root := t.TempDir()
	rel := "specs/capabilities/media-files/spec.md"
	dir := filepath.Join(root, "specs", "capabilities", "media-files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(path, []byte("# Capability: media-files\n\nOld text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return &weave.Graph{}, nil }})
	post := func(body map[string]string, token bool) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		req := post("/api/spec-save", bytes.NewReader(raw))
		if token {
			req.Header.Set("X-Weave-Token", "tok")
		}
		h.ServeHTTP(rec, req)
		return rec
	}

	if got := post(map[string]string{"path": rel, "text": "x"}, false); got.Code != 403 {
		t.Errorf("without token: %d, want 403", got.Code)
	}
	for _, bad := range []map[string]string{
		{"path": "specs/../x.md", "text": "x"},
		{"path": "src/a.ts", "text": "x"},
		{"path": rel, "text": strings.Repeat("x", 512*1024+1)},
	} {
		if got := post(bad, true); got.Code != 400 {
			t.Errorf("%v: %d, want 400", bad["path"], got.Code)
		}
	}

	newText := "# Capability: media-files\n\nEdited by the panel.\n"
	if got := post(map[string]string{"path": rel, "text": newText}, true); got.Code != 200 {
		t.Fatalf("save: %d %s", got.Code, got.Body.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != newText {
		t.Fatalf("file after save: %q %v", raw, err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/spec?path="+rel))
	var page struct{ Text, HTML string }
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Text != newText || !strings.Contains(page.HTML, "Edited by the panel.") {
		t.Errorf("page after save: %+v", page)
	}
}

// A repo whose specs/ is itself a link out of the repo serves nothing from it.
func TestSpecRoutesRefuseLinkedSpecsDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "spec.md")
	const secretText = "# outside secret\n"
	if err := os.WriteFile(secret, []byte(secretText), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "specs")
	if err := os.Symlink(outside, link); err != nil {
		if err := junction(outside, link); err != nil {
			t.Skipf("cannot create a symlink or junction here: %v", err)
		}
	}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return &weave.Graph{}, nil }})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/spec?path="+url.QueryEscape("specs/spec.md")))
	if rec.Code != 400 || strings.Contains(rec.Body.String(), "outside secret") {
		t.Errorf("read through a linked specs dir: %d %s", rec.Code, rec.Body.String())
	}

	raw, err := json.Marshal(map[string]string{"path": "specs/spec.md", "text": "# pwned\n"})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req := post("/api/spec-save", bytes.NewReader(raw))
	req.Header.Set("X-Weave-Token", "tok")
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("save through a linked specs dir: %d %s", rec.Code, rec.Body.String())
	}
	if got, err := os.ReadFile(secret); err != nil || string(got) != secretText {
		t.Errorf("a refused save changed the outside file: %q %v", got, err)
	}
}

// A link planted under specs/ must not make the renderer read, or the editor write,
// a file outside the repo. The link can be the last element or a directory on the
// way in. A junction stands in for a symlink where Windows withholds the privilege.
func TestSpecRoutesRefuseLinksOutOfSpecs(t *testing.T) {
	root := t.TempDir()
	realSpec := filepath.Join(root, "specs", "changes", "2026-01-01-a", "tasks.md")
	if err := os.MkdirAll(filepath.Dir(realSpec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realSpec, []byte("# Tasks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.md")
	const secretText = "# outside secret\n"
	if err := os.WriteFile(secret, []byte(secretText), 0o644); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return &weave.Graph{}, nil }})

	read := func(rel string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, get("/api/spec?path="+url.QueryEscape(rel)))
		return rec
	}
	save := func(rel, text string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(map[string]string{"path": rel, "text": text})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		req := post("/api/spec-save", bytes.NewReader(raw))
		req.Header.Set("X-Weave-Token", "tok")
		h.ServeHTTP(rec, req)
		return rec
	}

	// A directory link, specs/leak -> outside, escapes both routes.
	dirLink := filepath.Join(root, "specs", "leak")
	if err := os.Symlink(outside, dirLink); err != nil {
		if err := junction(outside, dirLink); err != nil {
			t.Skipf("cannot create a symlink or junction here: %v", err)
		}
	}
	leak := "specs/leak/secret.md"
	if rec := read(leak); rec.Code != 400 || strings.Contains(rec.Body.String(), "outside secret") {
		t.Errorf("read through a directory link: %d %s", rec.Code, rec.Body.String())
	}
	if rec := save(leak, "# pwned\n"); rec.Code != 400 {
		t.Errorf("save through a directory link: %d %s", rec.Code, rec.Body.String())
	}
	if raw, err := os.ReadFile(secret); err != nil || string(raw) != secretText {
		t.Errorf("a refused save changed the outside file: %q %v", raw, err)
	}

	// A file link, specs/link.md -> outside/secret.md, escapes the read route (the
	// save renames over the link, but reading it is still a leak).
	if err := os.Symlink(secret, filepath.Join(root, "specs", "link.md")); err == nil {
		if rec := read("specs/link.md"); rec.Code != 400 || strings.Contains(rec.Body.String(), "outside secret") {
			t.Errorf("read through a file link: %d %s", rec.Code, rec.Body.String())
		}
		if rec := save("specs/link.md", "# pwned\n"); rec.Code != 400 {
			t.Errorf("save through a file link: %d %s", rec.Code, rec.Body.String())
		}
	}

	// Legitimate work is untouched: an existing spec reads and edits, and a path
	// that does not exist is refused as before — the editor replaces files, it does
	// not create them (new change records come from /api/spec-new).
	if rec := read("specs/changes/2026-01-01-a/tasks.md"); rec.Code != 200 {
		t.Errorf("existing spec: %d %s", rec.Code, rec.Body.String())
	}
	if rec := save("specs/changes/2026-01-01-a/tasks.md", "# Tasks, edited\n"); rec.Code != 200 {
		t.Fatalf("existing spec save: %d %s", rec.Code, rec.Body.String())
	}
	if raw, err := os.ReadFile(realSpec); err != nil || string(raw) != "# Tasks, edited\n" {
		t.Errorf("edited spec on disk: %q %v", raw, err)
	}
	fresh := "specs/changes/2026-01-01-a/new.md"
	if rec := save(fresh, "# New spec\n"); rec.Code != 400 {
		t.Errorf("nonexistent spec: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(fresh))); err == nil {
		t.Error("a refused path must not create a file")
	}
}

// /api/specs reads capability specs through the pinned specs root and bounds each
// read: a linked area is not followed out of specs, and an overlarge spec is skipped
// instead of read whole.
func TestSpecsCapabilitiesStayConfinedAndBounded(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "specs", "capabilities", "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realMD := "# Capability: real\n\n## Parts\n\n| Path | What it does |\n| --- | --- |\n| `src/a.ts` | Serves. |\n"
	if err := os.WriteFile(filepath.Join(realDir, "spec.md"), []byte(realMD), 0o644); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	leakMD := "# Capability: leak\n\n## Parts\n\n| Path | What it does |\n| --- | --- |\n| `secret/leak.ts` | Leaks. |\n"
	if err := os.WriteFile(filepath.Join(outside, "spec.md"), []byte(leakMD), 0o644); err != nil {
		t.Fatal(err)
	}
	leak := filepath.Join(root, "specs", "capabilities", "leak")
	if err := os.Symlink(outside, leak); err != nil {
		if err := junction(outside, leak); err != nil {
			t.Skipf("cannot create a symlink or junction here: %v", err)
		}
	}

	hugeDir := filepath.Join(root, "specs", "capabilities", "overlarge")
	if err := os.MkdirAll(hugeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	huge := "# Capability: overlarge\n\n## Parts\n\n" + strings.Repeat("| `x.ts` | y |\n", 40000)
	if err := os.WriteFile(filepath.Join(hugeDir, "spec.md"), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}

	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return &weave.Graph{}, nil }})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/specs"))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"name":"real"`) {
		t.Errorf("the real capability is missing: %s", body)
	}
	for _, bad := range []string{"leak", "overlarge", "secret/leak.ts"} {
		if strings.Contains(body, bad) {
			t.Errorf("/api/specs served %q: %s", bad, body)
		}
	}
}
