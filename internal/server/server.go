// Package server serves the explorer and the wiki from one process, and lets the
// page do the things a person actually needs: run the gate, open the file behind a
// failure, leave a note, tick a task or start a change, regenerate the wiki.
//
// It binds loopback only and refuses cross-origin writes. It is not a team server and
// must not become one: the product's claim is a local gate, not a hosted dashboard.
package server

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/delta"
	"github.com/michalbasisty/weave/internal/weave"
)

//go:embed assets/app.js assets/app.css
var assets embed.FS

// Config is everything the handler needs. Graph is called per request so a stale
// index is rebuilt before the page is drawn, the same rule the MCP server follows.
type Config struct {
	Root  string
	Graph func() (*weave.Graph, error)
	Token string
	// Projects, when set, lists every project this server can show, for the page's
	// project switcher. Each one's Href is where its panel lives.
	Projects func() []Project
}

// Project is one entry in the switcher.
type Project struct {
	Name, Root, Href string
}

// NewToken is the per-run write token embedded in the page.
func NewToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "weave"
	}
	return hex.EncodeToString(b[:])
}

func (c Config) bin() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "weave"
}

// run executes a weave subcommand and returns its combined output.
func (c Config) run(args ...string) (string, int) {
	cmd := exec.Command(c.bin(), args...)
	cmd.Dir = c.Root
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return err.Error(), 1
		}
	}
	return string(out), code
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// localOnly rejects a request a browser on another origin could have forged, and a
// Host header that is not loopback (DNS rebinding).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			if !strings.HasPrefix(o, "http://127.0.0.1") && !strings.HasPrefix(o, "http://localhost") {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		if h := r.Host; h != "" {
			name := h
			if i := strings.LastIndexByte(name, ':'); i > 0 {
				name = name[:i]
			}
			if name != "127.0.0.1" && name != "localhost" && name != "[::1]" {
				http.Error(w, "host refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (c Config) authed(r *http.Request) bool {
	if c.Token == "" {
		return true
	}
	return r.Header.Get("X-Weave-Token") == c.Token || r.URL.Query().Get("t") == c.Token
}

// Handler wires every route. One panel: `/` is the app, `/wiki/` is the generated
// documentation, `/api/*` is what the page can do.
func Handler(c Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		g, err := c.Graph()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ng := weave.NamedGraph{Name: filepath.Base(c.Root), Graph: g}
		// "Since the last commit" is the first thing a person checking an agent's work
		// wants, so the hosted page computes it the same way `weave explore` does.
		if base := baseline.GraphAt(c.Root, "HEAD"); base != nil {
			ng.Delta = delta.Explore(g, base, "HEAD")
		}
		page := weave.RenderExplore([]weave.NamedGraph{ng})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, inject(page, c.Token, c.projectList()))
	})

	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := assets.ReadFile("assets/app.js")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/app.css", func(w http.ResponseWriter, r *http.Request) {
		b, _ := assets.ReadFile("assets/app.css")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(b)
	})

	wikiDir := filepath.Join(c.Root, ".weave", "wiki")
	mux.Handle("/wiki/", http.StripPrefix("/wiki/", http.FileServer(http.Dir(wikiDir))))

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		g, err := c.Graph()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		counts := map[string]int{}
		for _, n := range g.Nodes {
			counts[n.Kind]++
		}
		writeJSON(w, 200, map[string]any{"root": c.Root, "builtAt": g.BuiltAt, "files": g.FileCount, "counts": counts})
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		g, err := c.Graph()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"name": filepath.Base(c.Root), "graph": json.RawMessage(g.MarshalJS())})
	})

	mux.HandleFunc("/api/check", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		out, code := c.run("check", "--root", c.Root)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	mux.HandleFunc("/api/reindex", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		out, code := c.run("index", "--root", c.Root)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	mux.HandleFunc("/api/wiki", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		out, code := c.run("wiki", "--root", c.Root)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out, "url": "/wiki/"})
	})

	// remember writes a note beside the files it concerns.
	mux.HandleFunc("/api/note", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		var body struct {
			Note  string   `json:"note"`
			Files []string `json:"files"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || strings.TrimSpace(body.Note) == "" || len(body.Files) == 0 {
			writeJSON(w, 400, map[string]string{"error": "note and files are required"})
			return
		}
		args := append([]string{"remember", "--note", body.Note, "--root", c.Root}, body.Files...)
		out, code := c.run(args...)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	// specs lists every change record with its task ids: the data the Specs drawer
	// edits. The graph is the source, so a record added by hand appears too.
	mux.HandleFunc("/api/specs", func(w http.ResponseWriter, r *http.Request) {
		g, err := c.Graph()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"changes": panelSpecs(g), "capabilities": panelCapabilities(c.Root, g), "views": panelViews(c.Root, g)})
	})

	// spec renders one spec file for people. Only Markdown under specs/ is served, and
	// the renderer escapes before it adds a tag. text is the source the editor starts
	// from.
	mux.HandleFunc("/api/spec", func(w http.ResponseWriter, r *http.Request) {
		rel, abs, ok := specFile(c.Root, r.URL.Query().Get("path"))
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "a path under specs/ ending .md"})
			return
		}
		info, err := os.Stat(abs)
		if err != nil || info.Size() > 512*1024 {
			writeJSON(w, 400, map[string]string{"error": "unreadable or larger than 512 KB"})
			return
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		md := string(raw)
		writeJSON(w, 200, map[string]any{"path": rel, "title": specTitle(md), "text": md, "html": renderSpecMD(md)})
	})

	// spec-save replaces one spec file: the panel's editor writes through the same lock
	// as the task writer and a temp file, so a reader never sees half a document.
	mux.HandleFunc("/api/spec-save", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		var body struct{ Path, Text string }
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			writeJSON(w, 400, map[string]string{"error": "path and text are required"})
			return
		}
		rel, abs, ok := specFile(c.Root, body.Path)
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "a path under specs/ ending .md"})
			return
		}
		if info, err := os.Stat(abs); err != nil || info.Size() > 512*1024 {
			writeJSON(w, 400, map[string]string{"error": "unreadable or larger than 512 KB"})
			return
		}
		if len(body.Text) > 512*1024 {
			writeJSON(w, 400, map[string]string{"error": "a spec is at most 512 KB"})
			return
		}
		if err := weave.SaveSpecText(c.Root, rel, body.Text); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "path": rel, "bytes": len(body.Text)})
	})

	// spec-new creates the three files a change needs, through `weave spec new`.
	mux.HandleFunc("/api/spec-new", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		var body struct{ Name, Title string }
		name := ""
		if json.NewDecoder(r.Body).Decode(&body) == nil {
			name = strings.TrimSpace(body.Name)
		}
		if !nameOK.MatchString(name) {
			writeJSON(w, 400, map[string]string{"error": "a name is letters, digits, dash or underscore: billing-retry"})
			return
		}
		args := []string{"spec", "new", name, "--root", c.Root}
		if strings.TrimSpace(body.Title) != "" {
			args = append(args, "--title", body.Title)
		}
		out, code := c.run(args...)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	// task-done ticks or reopens one task in a change record, through the same
	// command the CLI uses. done defaults to true for an older page.
	mux.HandleFunc("/api/task-done", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		var body struct {
			Change, Task string
			Done         *bool
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !nameOK.MatchString(body.Change) || !nameOK.MatchString(body.Task) {
			writeJSON(w, 400, map[string]string{"error": "change and task are required"})
			return
		}
		sub := "done"
		if body.Done != nil && !*body.Done {
			sub = "reopen"
		}
		out, code := c.run("spec", "task", sub, body.Change, body.Task, "--root", c.Root)
		writeJSON(w, 200, map[string]any{"exit": code, "output": out})
	})

	// file opens one window of a file, never the whole file.
	mux.HandleFunc("/api/review", func(w http.ResponseWriter, r *http.Request) {
		g, err := c.Graph()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, buildReview(g, c.Root))
	})

	// review/mark closes the range: everything before this commit counts as seen.
	mux.HandleFunc("/api/review/mark", func(w http.ResponseWriter, r *http.Request) {
		if !c.authed(r) {
			writeJSON(w, 403, map[string]string{"error": "missing token"})
			return
		}
		head := gitOut(c.Root, "rev-parse", "HEAD")
		if head == "" {
			writeJSON(w, 400, map[string]string{"error": "no git history to mark"})
			return
		}
		if err := appendMark(c.Root, head); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"marked": head})
	})
	mux.HandleFunc("/api/file", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		line, _ := strconv.Atoi(r.URL.Query().Get("line"))
		if rel == "" {
			writeJSON(w, 400, map[string]string{"error": "path is required"})
			return
		}
		full := filepath.Join(c.Root, filepath.FromSlash(rel))
		abs, err := filepath.Abs(full)
		rootAbs, _ := filepath.Abs(c.Root)
		if err != nil || !strings.HasPrefix(abs, rootAbs) {
			writeJSON(w, 400, map[string]string{"error": "outside the repo"})
			return
		}
		if strings.Contains(rel, "node_modules/") || strings.HasPrefix(filepath.Base(rel), ".") {
			writeJSON(w, 400, map[string]string{"error": "not served"})
			return
		}
		info, err := os.Stat(abs)
		if err != nil || info.Size() > 512*1024 {
			writeJSON(w, 400, map[string]string{"error": "unreadable or larger than 512 KB"})
			return
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		start, end := 0, len(lines)
		if line > 0 {
			start = line - 26
			if start < 0 {
				start = 0
			}
			end = line + 44
			if end > len(lines) {
				end = len(lines)
			}
		} else if end > 200 {
			end = 200
		}
		numbered := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			numbered = append(numbered, fmt.Sprintf("%5d  %s", i+1, lines[i]))
		}
		writeJSON(w, 200, map[string]any{"path": rel, "start": start + 1, "text": strings.Join(numbered, "\n"), "total": len(lines)})
	})

	return localOnly(mux)
}

// nameOK guards the two values a browser passes to `weave spec`: a change slug and
// a task id. No separator and no leading dot, so the command cannot leave
// specs/changes.
var nameOK = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// specFile resolves a browser-supplied Markdown path to a file under specs/. No
// traversal, no separator, no other extension.
func specFile(root, path string) (string, string, bool) {
	rel := filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(path, "./")))
	if !strings.HasPrefix(rel, "specs/") || !strings.HasSuffix(rel, ".md") || strings.Contains(rel, "..") || strings.Contains(rel, `\`) {
		return "", "", false
	}
	abs, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel)))
	rootAbs, _ := filepath.Abs(root)
	if err != nil || !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		return "", "", false
	}
	return rel, abs, true
}

type panelTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

type panelChange struct {
	Slug  string      `json:"slug"`
	File  string      `json:"file,omitempty"`
	Done  int         `json:"done"`
	Total int         `json:"total"`
	Tasks []panelTask `json:"tasks"`
}

type panelCapability struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Parts int    `json:"parts"`
	Reqs  int    `json:"reqs"`
	Line  int    `json:"line,omitempty"`
}

type panelView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	File      string   `json:"file,omitempty"`
	Owner     string   `json:"owner,omitempty"`
	Endpoints []string `json:"endpoints"`
	Tables    []string `json:"tables"`
}

// panelSpecs lists change records newest first, with the task id each tick needs.
func panelSpecs(g *weave.Graph) []panelChange {
	ix := weave.NewIndex(g)
	out := []panelChange{}
	for _, c := range ix.OfKind(weave.KindChange) {
		ch := panelChange{Slug: strings.TrimPrefix(c.ID, "change:"), Tasks: []panelTask{}}
		if c.File != nil {
			ch.File = *c.File
		}
		for _, e := range ix.From(c.ID, "has_task") {
			t := ix.Node(e.To)
			if t == nil {
				continue
			}
			id := strings.TrimPrefix(t.ID, "task:")
			if i := strings.LastIndexByte(id, '/'); i >= 0 {
				id = id[i+1:]
			}
			pt := panelTask{ID: id}
			if t.Meta != nil {
				if v, ok := t.Meta.Get("title"); ok {
					pt.Title = v.Str()
				}
				if v, ok := t.Meta.Get("done"); ok {
					pt.Done = v.Bool()
				}
			}
			// The evidence clause belongs to the task's file list, not to its label.
			if i := strings.Index(pt.Title, " Evidence:"); i >= 0 {
				pt.Title = strings.TrimSpace(pt.Title[:i])
			}
			ch.Tasks = append(ch.Tasks, pt)
			ch.Total++
			if pt.Done {
				ch.Done++
			}
		}
		out = append(out, ch)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Slug > out[j].Slug })
	return out
}

// panelCapabilities lists specs/capabilities/<area>/spec.md. weave indexes their
// requirements but not the capability itself, so the name and the Parts count come
// from the files; reqs come from the graph.
func panelCapabilities(root string, g *weave.Graph) []panelCapability {
	byFile := map[string]int{}
	for _, n := range g.Nodes {
		if n.Kind == weave.KindReq && n.File != nil {
			byFile[*n.File]++
		}
	}
	out := []panelCapability{}
	for _, s := range capabilitySpecs(root) {
		parts, line := partsTable(s.md)
		out = append(out, panelCapability{Name: s.name, File: s.file, Parts: parts, Reqs: byFile[s.file], Line: line})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type capSpec struct {
	name, file, md string
}

func capabilitySpecs(root string) []capSpec {
	entries, err := os.ReadDir(filepath.Join(root, "specs", "capabilities"))
	if err != nil {
		return nil
	}
	out := []capSpec{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := "specs/capabilities/" + e.Name() + "/spec.md"
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		out = append(out, capSpec{name: e.Name(), file: rel, md: string(raw)})
	}
	return out
}

// panelViews is the cross-reference people ask for: every screen, the endpoints and
// tables it reaches, and the capability spec whose Parts table names its file.
func panelViews(root string, g *weave.Graph) []panelView {
	ix := weave.NewIndex(g)
	flows := map[string]weave.Flow{}
	for _, f := range weave.ExplorePayload(g).Flows {
		flows[f.P] = f
	}
	parts := capabilityParts(root)
	out := []panelView{}
	for _, p := range ix.OfKind(weave.KindPage) {
		v := panelView{ID: p.ID, Name: p.Name, Endpoints: []string{}, Tables: []string{}}
		if p.File != nil {
			v.File = *p.File
			v.Owner = ownerOf(parts, *p.File)
		}
		f := flows[p.ID]
		for _, id := range f.E {
			if n := ix.Node(id); n != nil {
				v.Endpoints = append(v.Endpoints, n.Name)
			}
		}
		for _, id := range f.T {
			if n := ix.Node(id); n != nil {
				v.Tables = append(v.Tables, n.Name)
			}
		}
		sort.Strings(v.Endpoints)
		sort.Strings(v.Tables)
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type capPart struct {
	path, capability string
}

// capabilityParts flattens every Parts table into path to capability. A cell may name
// several paths, so each backticked span counts.
func capabilityParts(root string) []capPart {
	out := []capPart{}
	for _, s := range capabilitySpecs(root) {
		for _, p := range partsPaths(s.md) {
			out = append(out, capPart{path: p, capability: s.name})
		}
	}
	return out
}

var reBacktickPath = regexp.MustCompile("`([^`]+)`")

func partsPaths(md string) []string {
	out := []string{}
	in := false
	for _, raw := range strings.Split(md, "\n") {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "## Parts") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "# ") {
			break
		}
		if !strings.HasPrefix(t, "|") {
			continue
		}
		cell := strings.TrimSpace(strings.TrimPrefix(t, "|"))
		if i := strings.IndexByte(cell, '|'); i >= 0 {
			cell = cell[:i]
		}
		for _, m := range reBacktickPath.FindAllStringSubmatch(cell, -1) {
			p := strings.TrimSuffix(strings.TrimSpace(m[1]), "/")
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// ownerOf picks the longest Parts path that names the file or a directory above it.
func ownerOf(parts []capPart, file string) string {
	best, bestLen := "", 0
	for _, p := range parts {
		if (file == p.path || strings.HasPrefix(file, p.path+"/")) && len(p.path) > bestLen {
			best, bestLen = p.capability, len(p.path)
		}
	}
	return best
}

// partsTable counts the data rows of the first table under "## Parts" and returns
// the line that heading is on, so the drawer opens the window there.
func partsTable(md string) (rows, line int) {
	in := false
	for i, raw := range strings.Split(md, "\n") {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "## Parts") {
			in, line = true, i+1
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "# ") {
			break
		}
		if cell := strings.TrimSpace(strings.TrimPrefix(t, "|")); strings.HasPrefix(cell, "`") {
			rows++
		}
	}
	return rows, line
}

// inject puts the action layer into the rendered page: a stylesheet, the script, and
// the write token the script uses.
// projectList is the switcher's data, with this server's own project marked.
func (c Config) projectList() []map[string]any {
	if c.Projects == nil {
		return nil
	}
	var out []map[string]any
	for _, p := range c.Projects() {
		out = append(out, map[string]any{"name": p.Name, "href": p.Href, "current": filepath.Clean(p.Root) == filepath.Clean(c.Root)})
	}
	return out
}

// inject adds the hosted layer. Its paths are relative, so the same page works at /
// and under /p/<name>/.
func inject(page, token string, projects []map[string]any) string {
	list, _ := json.Marshal(projects)
	if projects == nil {
		list = []byte("[]")
	}
	layer := `<link rel="stylesheet" href="app.css">
<script>window.__weave={token:"` + token + `",projects:` + string(list) + `}</script>
<script src="app.js"></script>
`
	if i := strings.LastIndex(page, "</body>"); i >= 0 {
		return page[:i] + layer + page[i:]
	}
	return page + layer
}

// Multi serves the primary project at / and every other known project at
// /p/<name>/, each with its own index, checks, specs and wiki, so one panel can
// switch between them. A project's handler is built on first use and kept.
func Multi(primary Config, open func(name string) (Config, bool)) http.Handler {
	main := Handler(primary)
	var mu sync.Mutex
	cache := map[string]http.Handler{}
	return localOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/p/")
		if !ok {
			main.ServeHTTP(w, r)
			return
		}
		name, _, slash := strings.Cut(rest, "/")
		if !slash {
			http.Redirect(w, r, "/p/"+name+"/?"+r.URL.RawQuery, http.StatusFound)
			return
		}
		mu.Lock()
		h, hit := cache[name]
		if !hit {
			if c, found := open(name); found {
				h = http.StripPrefix("/p/"+name, Handler(c))
				cache[name] = h
			}
		}
		mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	}))
}
