package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/weave"
)

func testHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &weave.Graph{Version: weave.GraphVersion, Root: root, FileCount: 1, Nodes: []*weave.GNode{{ID: "file:a.ts", Kind: weave.KindFile, Name: "a.ts"}}}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return g, nil }})
	return h, root
}

// The page must carry the action layer and the write token, or none of the buttons
// in it can work.
func TestPageCarriesTheActionLayer(t *testing.T) {
	h, _ := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/"))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"/app.js", "/app.css", `window.__weave={token:"tok"}`} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

// A write without the token is refused: the page has the token, a forged request does not.
func TestWritesNeedTheToken(t *testing.T) {
	h, _ := testHandler(t)
	for _, path := range []string{"/api/check", "/api/note", "/api/wiki", "/api/task-done", "/api/spec-new", "/api/spec-save"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, post(path, strings.NewReader(`{}`)))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without a token: got %d, want 403", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := post("/api/note", strings.NewReader(`{"note":"x"}`))
	req.Header.Set("X-Weave-Token", "tok")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("note without files should be a 400, got %d", rec.Code)
	}
}

// A browser on another origin cannot drive the local server.
func TestCrossOriginRefused(t *testing.T) {
	h, _ := testHandler(t)
	rec := httptest.NewRecorder()
	req := get("/")
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin got %d, want 403", rec.Code)
	}
}

// /api/file answers one window of a file, refuses paths outside the repo and dotfiles.
func TestFileWindowAndGuards(t *testing.T) {
	h, root := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/file?path=a.ts"))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Path, Text string
		Total      int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Text, "export const a = 1") || got.Total != 2 {
		t.Errorf("unexpected window: %+v", got)
	}

	for _, bad := range []string{"../outside.ts", ".env", "node_modules/x.ts"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/file?path="+bad, nil))
		if rec.Code == 200 {
			t.Errorf("%s should not be served", bad)
		}
	}
	_ = root
}

func TestStatusReportsCounts(t *testing.T) {
	h, root := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/status"))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Root   string
		Counts map[string]int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Root != root || got.Counts[weave.KindFile] != 1 {
		t.Errorf("unexpected status: %+v", got)
	}
}

func TestTokenIsRandomPerRun(t *testing.T) {
	a, b := NewToken(), NewToken()
	if len(a) != 32 || a == b {
		t.Fatalf("tokens: %q %q", a, b)
	}
}

// get and post are the test requests a loopback client would make: same path, a
// loopback Host, because the server refuses any other host.
func get(path string) *http.Request {
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "127.0.0.1:7777"
	return r
}

func post(path string, body io.Reader) *http.Request {
	r := httptest.NewRequest("POST", path, body)
	r.Host = "127.0.0.1:7777"
	return r
}

// A Host that is not loopback is refused: DNS rebinding cannot turn a page into a
// client of this server.
func TestNonLoopbackHostRefused(t *testing.T) {
	h, _ := testHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Host = "evil.example:7777"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rec.Code)
	}
}

// The review sheet reports the mark, and marking stores it so the range closes.
func TestReviewMarksAndRange(t *testing.T) {
	h, root := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/review"))
	if rec.Code != 200 {
		t.Fatalf("review status %d", rec.Code)
	}
	var got Review
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Head != "" {
		t.Errorf("a temp dir has no git history, head should be empty: %q", got.Head)
	}

	// marking without git is a clear 400, not a silent success
	rec = httptest.NewRecorder()
	req := post("/api/review/mark", strings.NewReader("{}"))
	req.Header.Set("X-Weave-Token", "tok")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mark without history: got %d, want 400", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, ".weave", "reviews.log")); err == nil {
		t.Error("a failed mark must not write the ledger")
	}
}

// The Specs drawer needs each task's id and state, newest change first.
func TestSpecsListsTasks(t *testing.T) {
	root := t.TempDir()
	done := &jsjson.Object{}
	done.Set("title", jsjson.Str("First task. Evidence: `a.ts`"))
	done.Set("done", jsjson.Bool(true))
	open := &jsjson.Object{}
	open.Set("title", jsjson.Str("Second task"))
	open.Set("done", jsjson.Bool(false))
	g := &weave.Graph{Version: weave.GraphVersion, Root: root, Nodes: []*weave.GNode{
		{ID: "change:2026-01-01-a", Kind: weave.KindChange, Name: "2026-01-01-a", File: weave.Ptr("specs/changes/2026-01-01-a/tasks.md")},
		{ID: "task:2026-01-01-a/T1", Kind: weave.KindTask, Name: "2026-01-01-a T1", Meta: done},
		{ID: "task:2026-01-01-a/T2", Kind: weave.KindTask, Name: "2026-01-01-a T2", Meta: open},
		{ID: "change:2025-12-31-b", Kind: weave.KindChange, Name: "2025-12-31-b"},
		{ID: "task:2025-12-31-b/T1", Kind: weave.KindTask, Name: "2025-12-31-b T1", Meta: open},
	}, Edges: []weave.GEdge{
		{From: "change:2026-01-01-a", To: "task:2026-01-01-a/T1", Kind: "has_task"},
		{From: "change:2026-01-01-a", To: "task:2026-01-01-a/T2", Kind: "has_task"},
		{From: "change:2025-12-31-b", To: "task:2025-12-31-b/T1", Kind: "has_task"},
	}}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return g, nil }})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/specs"))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Changes []struct {
			Slug, File  string
			Done, Total int
			Tasks       []struct {
				ID, Title string
				Done      bool
			}
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Changes) != 2 || got.Changes[0].Slug != "2026-01-01-a" {
		t.Fatalf("newest first, got %+v", got.Changes)
	}
	c := got.Changes[0]
	if c.Done != 1 || c.Total != 2 || len(c.Tasks) != 2 || c.Tasks[0].ID != "T1" || c.Tasks[1].ID != "T2" || !c.Tasks[0].Done || c.Tasks[0].Title != "First task." || c.Tasks[1].Title != "Second task" {
		t.Errorf("unexpected change: %+v", c)
	}
}

// A browser cannot aim `weave spec` outside specs/changes or smuggle arguments in.
func TestSpecWritesRefuseBadNames(t *testing.T) {
	h, root := testHandler(t)
	for _, body := range []string{`{"name":"../evil"}`, `{"name":"a/b"}`, `{"name":""}`, `{"name":".hidden"}`} {
		rec := httptest.NewRecorder()
		req := post("/api/spec-new", strings.NewReader(body))
		req.Header.Set("X-Weave-Token", "tok")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, rec.Code)
		}
	}
	for _, body := range []string{`{}`, `{"change":"../x","task":"T1"}`, `{"change":"a","task":"T1!rm"}`, `{"change":"a","task":"T1; rm"}`} {
		rec := httptest.NewRecorder()
		req := post("/api/task-done", strings.NewReader(body))
		req.Header.Set("X-Weave-Token", "tok")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, rec.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "specs")); err == nil {
		t.Error("a refused name must not create specs/")
	}
}

// A repo that keeps capability specs gets them listed even though weave makes no
// capability node: the Parts table and the requirement edges are the data.
func TestSpecsListsCapabilities(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "capabilities", "media-files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "# Capability: media-files\n\n## Parts\n\n| Path | What it does |\n| ---- | ------------ |\n| `src/a.ts` | Serves bytes. |\n| `src/b.ts` | Signs links. |\n\n## How to use this table\n"
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	file := "specs/capabilities/media-files/spec.md"
	g := &weave.Graph{Version: weave.GraphVersion, Root: root, Nodes: []*weave.GNode{
		{ID: "req:REQ-MEDIA-1", Kind: weave.KindReq, Name: "REQ-MEDIA-1", File: weave.Ptr(file)},
	}}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return g, nil }})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/specs"))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Capabilities []struct {
			Name, File  string
			Parts, Reqs int
			Line        int
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Capabilities) != 1 {
		t.Fatalf("capabilities: %+v", got.Capabilities)
	}
	c := got.Capabilities[0]
	if c.Name != "media-files" || c.File != file || c.Parts != 2 || c.Reqs != 1 || c.Line != 3 {
		t.Errorf("unexpected capability: %+v", c)
	}
}

// The Specs view cross-references every screen with the endpoints and tables it
// reaches, and with the capability whose Parts table names its file (longest prefix).
func TestSpecsListsViewsAndOwners(t *testing.T) {
	root := t.TempDir()
	writeSpec := func(area string, parts ...string) {
		dir := filepath.Join(root, "specs", "capabilities", area)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		rows := ""
		for _, p := range parts {
			rows += "| `" + p + "` | Does a thing. |\n"
		}
		md := "# Capability: " + area + "\n\n## Parts\n\n| Path | What it does |\n| ---- | --- |\n" + rows
		if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSpec("web", "packages/web")
	writeSpec("login", "packages/web/src/pages/Login/Login.tsx")
	writeSpec("other", "packages/backend/src/modules/other")

	file := "packages/web/src/pages/Login/Login.tsx"
	meta := &jsjson.Object{}
	meta.Set("module", jsjson.Str("auth"))
	g := &weave.Graph{Version: weave.GraphVersion, Root: root, Nodes: []*weave.GNode{
		{ID: "page:/login", Kind: weave.KindPage, Name: "/login", File: weave.Ptr(file)},
		{ID: "endpoint:POST /api/v1/login", Kind: weave.KindEndpoint, Name: "POST /api/v1/login", Meta: meta},
		{ID: "module:auth", Kind: weave.KindModule, Name: "auth"},
		{ID: "model:Session", Kind: weave.KindModel, Name: "Session"},
		{ID: "table:Session", Kind: weave.KindTable, Name: "Session"},
	}, Edges: []weave.GEdge{
		{From: "page:/login", To: "endpoint:POST /api/v1/login", Kind: "calls", Conf: weave.ConfExact},
		{From: "endpoint:POST /api/v1/login", To: "module:auth", Kind: "in_module", Conf: weave.ConfExact},
		{From: "module:auth", To: "model:Session", Kind: "uses", Conf: weave.ConfExact},
		{From: "model:Session", To: "table:Session", Kind: "maps_to", Conf: weave.ConfExact},
	}}
	h := Handler(Config{Root: root, Token: "tok", Graph: func() (*weave.Graph, error) { return g, nil }})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get("/api/specs"))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Views []struct {
			ID, Name, Owner string
			Endpoints       []string
			Tables          []string
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Views) != 1 {
		t.Fatalf("views: %+v", got.Views)
	}
	v := got.Views[0]
	if v.Name != "/login" || v.Owner != "login" {
		t.Errorf("view/owner: %+v", v)
	}
	if len(v.Endpoints) != 1 || v.Endpoints[0] != "POST /api/v1/login" {
		t.Errorf("endpoints: %v", v.Endpoints)
	}
	if len(v.Tables) != 1 || v.Tables[0] != "Session" {
		t.Errorf("tables: %v", v.Tables)
	}
}
