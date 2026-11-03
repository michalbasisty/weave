package weave

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/runners"
)

// The wiki is documentation, so it follows what documentation sites converged on:
// a persistent sidebar that mirrors the structure, search always one keystroke away,
// one page per question, the code next to the claim it supports, and a way forward at
// the foot of every page. Every page is generated from the same index the gate reads;
// regenerating is the only way to change it, and the set is stamped with its commit.

// WikiOptions is where and from what the site is written.
type WikiOptions struct {
	OutDir string
	Commit string
	Ref    string
}

// WikiResult is what was written.
type WikiResult struct {
	Pages int
	Index string
}

// navKind is one sidebar section.
type navKind struct {
	kind  string
	label string
	base  string // directory under the output root
}

var wikiKinds = []navKind{
	{KindPage, "Screens", "s"},
	{KindEndpoint, "Endpoints", "e"},
	{KindModule, "Modules", "m"},
	{KindTable, "Tables", "t"},
	{KindChange, "Changes", "c"},
}

type wikiWriter struct {
	ix    *Index
	g     *Graph
	opt   WikiOptions
	repo  string
	pages []string

	// per-page state
	rel  string
	kind string
	toc  []string
	prev string
	next string
}

// WriteWiki renders the site under opt.OutDir.
func WriteWiki(g *Graph, opt WikiOptions) (*WikiResult, error) {
	if opt.OutDir == "" {
		opt.OutDir = filepath.Join(g.Root, ".weave", "wiki")
	}
	if opt.Ref == "" && opt.Commit != "" {
		opt.Ref = opt.Commit
		if len(opt.Ref) > 7 {
			opt.Ref = opt.Ref[:7]
		}
	}
	w := &wikiWriter{ix: NewIndex(g), g: g, opt: opt, repo: filepath.Base(g.Root)}
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return nil, err
	}
	for name, body := range map[string]string{"wiki.css": wikiCSS, "wiki.js": wikiJS} {
		if err := os.WriteFile(filepath.Join(opt.OutDir, name), []byte(body), 0o644); err != nil {
			return nil, err
		}
	}
	// One shared search index, loaded as a script so it works from file:// and is
	// cached once instead of being inlined into 488 pages.
	if err := os.WriteFile(filepath.Join(opt.OutDir, "search.js"), []byte("window.W="+w.searchIndex("")+";\n"), 0o644); err != nil {
		return nil, err
	}

	w.each(KindModule, func(n *GNode) { w.write("m/"+Slug(n.Name)+".html", w.modulePage(n)) })
	w.each(KindEndpoint, func(n *GNode) { w.write("e/"+Slug(n.Name)+".html", w.endpointPage(n)) })
	w.each(KindPage, func(n *GNode) { w.write("s/"+Slug(n.Name)+".html", w.screenPage(n)) })
	w.each(KindTable, func(n *GNode) { w.write("t/"+Slug(n.Name)+".html", w.tablePage(n)) })
	w.each(KindChange, func(n *GNode) { w.write("c/"+Slug(n.Name)+".html", w.changePage(n)) })
	w.rel, w.kind, w.toc = "index.html", "", nil
	w.prev, w.next = "", ""
	w.write("index.html", w.indexPage())
	if err := w.writeStamp(); err != nil {
		return nil, err
	}
	return &WikiResult{Pages: len(w.pages) + 1, Index: filepath.Join(opt.OutDir, "index.html")}, nil
}

// each renders one kind in a stable order, so prev/next is the order a reader sees.
func (w *wikiWriter) each(kind string, fn func(*GNode)) {
	nodes := w.ix.OfKind(kind)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	for i, n := range nodes {
		w.rel, w.kind, w.toc = sectionDir(kind)+"/"+Slug(n.Name)+".html", kind, nil
		w.prev, w.next = "", ""
		if i > 0 {
			w.prev = nodes[i-1].Name
		}
		if i < len(nodes)-1 {
			w.next = nodes[i+1].Name
		}
		fn(n)
	}
}

// Slug is a stable file name for a node id: `GET /api/v1/tasks/:id` → `get-api-v1-tasks-id`.
func Slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "page"
	}
	return out
}

func (w *wikiWriter) href(from, to string) string {
	return strings.Repeat("../", strings.Count(from, "/")) + to
}

func (w *wikiWriter) url(from, kind, id string) string {
	n := w.ix.Node(id)
	switch kind {
	case KindModule:
		return w.href(from, "m/"+Slug(idName(n, id))+".html")
	case KindEndpoint:
		return w.href(from, "e/"+Slug(idName(n, id))+".html")
	case KindPage:
		return w.href(from, "s/"+Slug(idName(n, id))+".html")
	case KindTable:
		return w.href(from, "t/"+Slug(idName(n, id))+".html")
	case KindChange:
		return w.href(from, "c/"+Slug(idName(n, id))+".html")
	case KindModel:
		for _, e := range w.ix.From(id, "maps_to") {
			if t := w.ix.Node(e.To); t != nil && t.Kind == KindTable {
				return w.href(from, "t/"+Slug(t.Name)+".html")
			}
		}
	}
	return ""
}

func idName(n *GNode, id string) string {
	if n == nil {
		return id
	}
	return n.Name
}

func (w *wikiWriter) link(from, id string) string {
	n := w.ix.Node(id)
	if n == nil {
		return esc(strings.TrimPrefix(id, "file:"))
	}
	label := n.Name
	if n.Kind == KindModel {
		for _, e := range w.ix.From(id, "maps_to") {
			if t := w.ix.Node(e.To); t != nil && t.Kind == KindTable {
				label = t.Name
			}
		}
	}
	if u := w.url(from, n.Kind, id); u != "" {
		return `<a href="` + u + `">` + esc(label) + `</a>`
	}
	return esc(label)
}

func (w *wikiWriter) write(rel, body string) {
	p := filepath.Join(w.opt.OutDir, filepath.FromSlash(rel))
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	if os.WriteFile(p, []byte(body), 0o644) == nil {
		w.pages = append(w.pages, rel)
	}
}

// section is a heading plus body; it also feeds the page's table of contents.
func (w *wikiWriter) section(title, body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	anchor := Slug(title)
	w.toc = append(w.toc, title)
	return `<section id="` + anchor + `"><h2>` + esc(title) + `</h2>` + "\n" + body + "</section>\n"
}

func list(items []string) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<ul class=\"rel\">\n")
	for _, it := range items {
		b.WriteString("<li>" + it + "</li>\n")
	}
	b.WriteString("</ul>\n")
	return b.String()
}

func emptyState(why string) string { return `<p class="empty">` + esc(why) + `</p>` }

// folded keeps a landing page scannable: a long list is one line until opened.
func folded(summary, body string) string {
	return `<details class="fold"><summary>` + esc(summary) + `</summary>` + "\n" + body + "</details>\n"
}

// mdInline escapes text and turns `backticked` spans into code, which is how task
// titles in tasks.md are written.
func mdInline(s string) string {
	out := esc(s)
	parts := strings.Split(out, "`")
	if len(parts) < 3 {
		return out
	}
	var b strings.Builder
	for i, p := range parts {
		if i%2 == 1 {
			b.WriteString("<code>" + p + "</code>")
		} else {
			b.WriteString(p)
		}
	}
	return b.String()
}

func (w *wikiWriter) links(from string, ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, w.link(from, id))
	}
	sort.Strings(out)
	return out
}

func (w *wikiWriter) targetIDs(id, kind string) []string {
	var out []string
	for _, e := range w.ix.From(id, kind) {
		if w.ix.Has(e.To) {
			out = append(out, e.To)
		}
	}
	return out
}

func (w *wikiWriter) sourceIDs(id, kind string) []string {
	var out []string
	for _, e := range w.ix.To(id, kind) {
		if w.ix.Has(e.From) {
			out = append(out, e.From)
		}
	}
	return out
}

func (w *wikiWriter) tested(id string) bool {
	return len(w.ix.To(id, "hits")) > 0 || len(w.ix.To(id, "covers")) > 0
}

func (w *wikiWriter) hasE2E(id string) bool { return len(w.ix.To(id, "exercises")) > 0 }

func (w *wikiWriter) nodeFiles(id string) []string {
	n := w.ix.Node(id)
	if n == nil || n.File == nil {
		return nil
	}
	return []string{*n.File}
}

func (w *wikiWriter) notes(id string) string {
	files := w.nodeFiles(id)
	if len(files) == 0 {
		return ""
	}
	notes := NotesFor(w.g.Root, files)
	if len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<ul class="notes">` + "\n")
	for _, n := range notes {
		b.WriteString("<li><span class=\"when\">" + esc(n.At[:min(10, len(n.At))]) + "</span> " + esc(n.Note) + "</li>\n")
	}
	b.WriteString("</ul>\n")
	return b.String()
}

func (w *wikiWriter) tableRows(headers []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("<table>\n<thead><tr>")
	for _, h := range headers {
		b.WriteString("<th>" + esc(h) + "</th>")
	}
	b.WriteString("</tr></thead>\n<tbody>\n")
	for _, r := range rows {
		b.WriteString("<tr>")
		for _, c := range r {
			b.WriteString("<td>" + c + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody>\n</table>\n")
	return b.String()
}

// excerpt is the code a page is about: the lines around the anchor, numbered. A wiki
// that never shows the code sends the reader back to the editor for the one thing a
// reference page should contain.
func (w *wikiWriter) excerpt(file string, line, before, after int) string {
	if file == "" || line <= 0 {
		return ""
	}
	if strings.Contains(file, "node_modules/") || strings.HasPrefix(filepath.Base(file), ".") {
		return ""
	}
	full := filepath.Join(w.g.Root, filepath.FromSlash(file))
	rootAbs, _ := filepath.Abs(w.g.Root)
	abs, err := filepath.Abs(full)
	if err != nil || !strings.HasPrefix(abs, rootAbs) {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil || info.Size() > 512*1024 {
		return ""
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	start := line - before
	if start < 1 {
		start = 1
	}
	end := line + after
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	b.WriteString(`<figure class="code"><figcaption>` + esc(file) + `:` + strconv.Itoa(start) + `</figcaption><pre>`)
	for i := start; i <= end; i++ {
		text := esc(lines[i-1])
		if i == line {
			b.WriteString(`<b>` + text + `</b>`)
		} else {
			b.WriteString(text)
		}
		if i < end {
			b.WriteByte('\n')
		}
	}
	b.WriteString("</pre></figure>")
	return b.String()
}

/* ---------------------------------------------------------------- chrome */

func (w *wikiWriter) chrome(rel, title, crumbs, main string) string {
	home := w.href(rel, "index.html")
	css := w.href(rel, "wiki.css")
	js := w.href(rel, "wiki.js")
	stamp := "no git history"
	if w.opt.Ref != "" {
		stamp = w.opt.Ref
	}
	pager := ""
	if w.prev != "" || w.next != "" {
		pager = `<nav class="pager" aria-label="Pages in this section">`
		if w.prev != "" {
			pager += `<a class="prev" href="` + w.href(rel, sectionDir(w.kind)+"/"+Slug(w.prev)+".html") + `"><span>Previous</span>` + esc(w.prev) + `</a>`
		}
		if w.next != "" {
			pager += `<a class="next" href="` + w.href(rel, sectionDir(w.kind)+"/"+Slug(w.next)+".html") + `"><span>Next</span>` + esc(w.next) + `</a>`
		}
		pager += `</nav>`
	}
	toc := ""
	if len(w.toc) > 1 {
		toc = `<aside class="toc" aria-label="On this page"><p>On this page</p><ul>`
		for _, t := range w.toc {
			toc += `<li><a href="#` + Slug(t) + `">` + esc(t) + `</a></li>`
		}
		toc += `</ul></aside>`
	}
	meta := `<p class="meta">` + esc(w.stampLine()) + `</p>`
	if w.opt.Commit != "" {
		meta = `<p class="meta">built from <code>` + esc(stamp) + `</code> · ` + esc(w.stampLine()) + `</p>`
	}

	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + esc(title) + ` · weave</title>
<link rel="stylesheet" href="` + css + `">
</head>
<body>
<a class="skip" href="#doc">Skip to content</a>
<header class="bar">
  <a class="brand" href="` + home + `">weave</a>
  <span class="repo">` + esc(w.repo) + `</span>
  <div class="find"><input id="q" type="search" placeholder="Search the wiki — /" autocomplete="off" spellcheck="false" aria-label="Search the wiki"><div id="hits" role="listbox" hidden></div></div>
  <span class="stamp">` + esc(stamp) + `</span>
</header>
<div class="shell">
` + w.nav(rel) + `
<main id="doc">
<nav class="crumbs">` + crumbs + `</nav>
` + main + `
` + pager + `
<footer>` + meta + `</footer>
</main>
` + toc + `
</div>
<script src="` + w.href(rel, "search.js") + `"></script>
<script src="` + js + `"></script>
</body>
</html>
`
}

func sectionDir(kind string) string {
	for _, k := range wikiKinds {
		if k.kind == kind {
			return k.base
		}
	}
	return ""
}

// nav is the reader's map. It shows the sections and the items of the one you are in,
// because a sidebar with 488 links is a worse table of contents, not a better one.
func (w *wikiWriter) nav(from string) string {
	var b strings.Builder
	b.WriteString("<nav class=\"side\" aria-label=\"Wiki\">\n")
	home := w.href(from, "index.html")
	cur := ""
	if w.kind == "" {
		cur = ` aria-current="page"`
	}
	b.WriteString(`<a class="side-home" href="` + home + `"` + cur + `>Overview</a>` + "\n")

	for _, k := range wikiKinds {
		nodes := w.ix.OfKind(k.kind)
		sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		cur := ""
		if k.kind == w.kind {
			cur = ` aria-current="page"`
		}
		b.WriteString(`<a class="side-sec" href="` + home + `#` + strings.ToLower(k.label) + `"` + cur + `>` + esc(k.label) + ` <span class="cn">` + strconv.Itoa(len(nodes)) + `</span></a>` + "\n")
		if k.kind != w.kind {
			continue
		}
		item := func(n *GNode) string {
			active := ""
			if n.Name == w.currentName() {
				active = ` aria-current="page"`
			}
			return `<li><a href="` + w.href(from, k.base+"/"+Slug(n.Name)+".html") + `"` + active + `>` + esc(n.Name) + `</a></li>` + "\n"
		}
		if k.kind == KindEndpoint {
			// 248 endpoints in one list is a worse table of contents, not a sidebar:
			// group them by the module that handles them.
			groups := map[string][]*GNode{}
			var names []string
			for _, n := range nodes {
				g := w.navGroup(k.kind, n)
				if _, ok := groups[g]; !ok {
					names = append(names, g)
				}
				groups[g] = append(groups[g], n)
			}
			sort.Strings(names)
			b.WriteString("<ul class=\"side-list\">\n")
			for _, g := range names {
				b.WriteString(`<li class="side-grp"><span>` + esc(g) + `</span><ul>` + "\n")
				for _, n := range groups[g] {
					b.WriteString(item(n))
				}
				b.WriteString("</ul></li>\n")
			}
			b.WriteString("</ul>\n")
			continue
		}
		b.WriteString("<ul class=\"side-list\">\n")
		for _, n := range nodes {
			b.WriteString(item(n))
		}
		b.WriteString("</ul>\n")
	}
	b.WriteString("</nav>\n")
	return b.String()
}

func (w *wikiWriter) navGroup(kind string, n *GNode) string {
	if kind != KindEndpoint {
		return ""
	}
	mods := w.ix.targets(n.ID, "in_module")
	if len(mods) == 0 {
		return "no module"
	}
	return mods[0].Name
}

func (w *wikiWriter) currentName() string {
	if w.rel == "" {
		return ""
	}
	base := filepath.Base(w.rel)
	base = strings.TrimSuffix(base, ".html")
	for _, n := range w.ix.OfKind(w.kind) {
		if Slug(n.Name) == base {
			return n.Name
		}
	}
	return ""
}

// searchIndex is every page in one array, as [name, kind, url] — terse because it is
// written once and loaded by every page.
func (w *wikiWriter) searchIndex(from string) string {
	type hit [3]string
	var hits []hit
	for _, k := range wikiKinds {
		for _, n := range w.ix.OfKind(k.kind) {
			hits = append(hits, hit{n.Name, strings.ToLower(strings.TrimSuffix(k.label, "s")), w.href(from, k.base+"/"+Slug(n.Name)+".html")})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i][0] < hits[j][0] })
	b, _ := json.Marshal(hits)
	return string(b)
}

func (w *wikiWriter) stampLine() string {
	c := w.g.Coverage
	if c == nil {
		return "coverage unknown — reindex with this version of weave"
	}
	pct := func(a, b int) string {
		if b == 0 {
			return "100"
		}
		return strconv.FormatFloat(float64(a)/float64(b)*100, 'f', 0, 64)
	}
	parts := []string{
		strconv.Itoa(c.Routes.Found) + " of " + strconv.Itoa(c.Routes.Files) + " route files read (" + pct(c.Routes.Found, c.Routes.Files) + "%)",
		strconv.Itoa(c.Calls.Resolved) + " of " + strconv.Itoa(c.Calls.Total) + " calls matched",
	}
	if c.NextPage != nil && c.NextPage.Files > 0 {
		parts = append(parts, "next pages "+strconv.Itoa(c.NextPage.Found)+"/"+strconv.Itoa(c.NextPage.Files))
	}
	return strings.Join(parts, " · ")
}

// wikiStamp records which commit the set describes, so a stale site is detected
// instead of trusted.
type wikiStamp struct {
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
	Pages   int    `json:"pages"`
}

func (w *wikiWriter) writeStamp() error {
	b, err := json.MarshalIndent(wikiStamp{Commit: w.opt.Commit, BuiltAt: isoNow(), Pages: len(w.pages) + 1}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.opt.OutDir, "stamp.json"), append(b, '\n'), 0o644)
}

// ReadWikiStamp is the recorded commit of a generated wiki, or "" when there is none.
func ReadWikiStamp(outDir string) string {
	raw, err := os.ReadFile(filepath.Join(outDir, "stamp.json"))
	if err != nil {
		return ""
	}
	var s wikiStamp
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s.Commit
}

/* ---------------------------------------------------------------- pages */

func (w *wikiWriter) endpointPage(n *GNode) string {
	rel := "e/" + Slug(n.Name) + ".html"
	moduleIDs := w.targetIDs(n.ID, "in_module")
	callers := w.sourceIDs(n.ID, "calls")
	screenIDs := []string{}
	for _, id := range callers {
		if c := w.ix.Node(id); c != nil && c.Kind == KindPage {
			screenIDs = append(screenIDs, id)
		}
	}
	tests := append(w.sourceIDs(n.ID, "hits"), w.sourceIDs(n.ID, "covers")...)

	lede := "An HTTP endpoint"
	if len(moduleIDs) > 0 {
		lede += " handled in " + w.link(rel, moduleIDs[0])
	}
	lede += ", called from " + plural(len(screenIDs), "screen") + ". "
	if w.tested(n.ID) {
		lede += "A test calls it directly."
	} else {
		lede += "No test calls it directly."
	}

	src := ""
	line := 0
	if n.File != nil {
		src = *n.File
		if n.Line != nil {
			line = *n.Line
		}
		if line > 0 {
			src += ":" + strconv.Itoa(line)
		}
	}
	body := `<h1>` + esc(n.Name) + `</h1>` + "\n<p class=\"lede\">" + lede + "</p>\n"
	body += w.section("Handler", w.excerpt(srcFile(n), line, 4, 26))
	body += w.section("Called from", list(w.links(rel, screenIDs)))
	if len(screenIDs) == 0 {
		body += w.section("Called from", emptyState("No screen calls this endpoint directly. A mobile client, a service worker or an agent may still use it."))
	}
	proof := list(w.links(rel, tests))
	if len(tests) == 0 {
		proof = emptyState("No test reaches this directly, so a change here is unproven. Give the module a test that calls it, or accept the gap knowingly.")
	}
	body += w.section("Proved by", proof)
	if cmds := w.commands(tests); cmds != "" {
		body += w.section("Run the proof", cmds)
	}
	reach := []string{}
	for _, m := range moduleIDs {
		reach = append(reach, w.links(rel, w.targetIDs(m, "uses"))...)
	}
	body += w.section("Reaches", list(reach))
	body += w.section("Source", `<p class="path"><code>`+esc(src)+`</code></p>`)
	body += w.section("Decisions", w.notes(n.ID))
	body += w.section("Planned in", w.plannedIn(rel, w.nodeFiles(n.ID)))

	return w.chrome(rel, n.Name, `<a href="`+w.href(rel, "index.html")+`">Endpoints</a> › `+esc(n.Name), body)
}

func srcFile(n *GNode) string {
	if n == nil || n.File == nil {
		return ""
	}
	return *n.File
}

func (w *wikiWriter) modulePage(n *GNode) string {
	rel := "m/" + Slug(n.Name) + ".html"
	tables := w.targetIDs(n.ID, "uses")
	allEps := []string{}
	for _, e := range w.ix.To(ModuleID(n.Name), "in_module") {
		allEps = append(allEps, e.From)
	}
	tests := []string{}
	for _, e := range allEps {
		tests = append(tests, w.sourceIDs(e, "hits")...)
		tests = append(tests, w.sourceIDs(e, "covers")...)
	}
	owned := w.sourceIDs(ModuleID(n.Name), "contains")

	lede := "A backend module: " + plural(len(allEps), "endpoint") + ", " + plural(len(tables), "table") + "."
	body := `<h1>` + esc(n.Name) + `</h1>` + "\n<p class=\"lede\">" + lede + "</p>\n"
	if len(allEps) > 0 {
		rows := make([][]string, 0, len(allEps))
		for _, id := range allEps {
			proof := `<span class="no">no test</span>`
			if w.tested(id) {
				proof = `<span class="yes">tested</span>`
			}
			rows = append(rows, []string{w.link(rel, id), proof})
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
		body += w.section("Endpoints it handles", w.tableRows([]string{"Endpoint", "Proof"}, rows))
	}
	body += w.section("Tables it uses", list(w.links(rel, tables)))
	body += w.section("Files", list(w.fileList(owned)))
	body += w.section("Tests", list(w.links(rel, tests)))
	if cmds := w.commands(tests); cmds != "" {
		body += w.section("Run the proof", cmds)
	}
	body += w.section("Decisions", w.notes(n.ID))
	return w.chrome(rel, n.Name, `<a href="`+w.href(rel, "index.html")+`">Modules</a> › `+esc(n.Name), body)
}

func (w *wikiWriter) screenPage(n *GNode) string {
	rel := "s/" + Slug(n.Name) + ".html"
	eps := w.targetIDs(n.ID, "calls")
	tests := w.sourceIDs(n.ID, "exercises")
	tables := []string{}
	for _, e := range eps {
		for _, m := range w.targetIDs(e, "in_module") {
			tables = append(tables, w.targetIDs(m, "uses")...)
		}
	}
	lede := "A screen calling " + plural(len(eps), "endpoint") + ". "
	if w.hasE2E(n.ID) {
		lede += "An end-to-end test drives it."
	} else {
		lede += "No end-to-end test drives it."
	}
	body := `<h1>` + esc(n.Name) + `</h1>` + "\n<p class=\"lede\">" + lede + "</p>\n"
	if n.File != nil {
		body += w.section("Component", w.excerpt(*n.File, 1, 1, 30))
	}
	body += w.section("Calls", list(w.links(rel, eps)))
	body += w.section("Reaches", list(w.links(rel, tables)))
	body += w.section("Driven by", list(w.links(rel, tests)))
	body += w.section("Decisions", w.notes(n.ID))
	return w.chrome(rel, n.Name, `<a href="`+w.href(rel, "index.html")+`">Screens</a> › `+esc(n.Name), body)
}

func (w *wikiWriter) tablePage(n *GNode) string {
	rel := "t/" + Slug(n.Name) + ".html"
	mods := w.sourceIDs(n.ID, "uses")
	models := w.sourceIDs(n.ID, "maps_to")
	eps := []string{}
	for _, m := range mods {
		for _, e := range w.ix.To(m, "in_module") {
			eps = append(eps, e.From)
		}
	}
	lede := "A database table"
	if len(models) > 0 {
		if m := w.ix.Node(models[0]); m != nil {
			lede += " from the model " + m.Name
		}
	}
	lede += ", used by " + plural(len(mods), "module") + "."
	body := `<h1>` + esc(n.Name) + `</h1>` + "\n<p class=\"lede\">" + lede + "</p>\n"
	body += w.section("Used by", list(w.links(rel, mods)))
	body += w.section("Reachable from", list(w.links(rel, eps)))
	body += w.section("Decisions", w.notes(n.ID))
	return w.chrome(rel, n.Name, `<a href="`+w.href(rel, "index.html")+`">Tables</a> › `+esc(n.Name), body)
}

func (w *wikiWriter) changePage(n *GNode) string {
	rel := "c/" + Slug(n.Name) + ".html"
	tasks := w.ix.targets(n.ID, "has_task")
	reqIDs := []string{}
	for _, r := range w.ix.targets(n.ID, "specified_in") {
		reqIDs = append(reqIDs, r.ID)
	}
	files := []string{}
	for _, t := range tasks {
		for _, e := range w.ix.From(t.ID, "touches") {
			if fn := w.ix.Node(e.To); fn != nil && fn.File != nil {
				files = append(files, *fn.File)
			}
		}
	}
	done, open := 0, 0
	var doneItems, openItems []string
	for _, t := range tasks {
		id := lastWord(t.Name)
		line := `<code>` + esc(id) + `</code> ` + mdInline(metaText(t, "title"))
		if metaTruthy(t, "done") {
			done++
			doneItems = append(doneItems, line)
		} else {
			open++
			openItems = append(openItems, line)
		}
	}
	lede := "A change record: " + strconv.Itoa(done) + " of " + strconv.Itoa(len(tasks)) + " tasks done"
	if open > 0 {
		lede += ", " + strconv.Itoa(open) + " open"
	}
	lede += "."
	body := `<h1>` + esc(n.Name) + `</h1>` + "\n<p class=\"lede\">" + lede + "</p>\n"
	body += w.section("Open tasks", list(openItems))
	body += w.section("Done", list(doneItems))
	body += w.section("Requirements", list(w.links(rel, reqIDs)))
	body += w.section("Files it names", list(uniqueSorted(files)))
	body += w.section("Decisions", w.notes(n.ID))
	return w.chrome(rel, n.Name, `<a href="`+w.href(rel, "index.html")+`">Changes</a> › `+esc(n.Name), body)
}

func (w *wikiWriter) plannedIn(rel string, files []string) string {
	if len(files) == 0 {
		return ""
	}
	var ids []string
	for _, c := range w.ix.OfKind(KindChange) {
		for _, t := range w.ix.targets(c.ID, "has_task") {
			for _, e := range w.ix.From(t.ID, "touches") {
				if n := w.ix.Node(e.To); n != nil && n.File != nil && contains(files, *n.File) {
					ids = append(ids, c.ID)
				}
			}
		}
	}
	return list(w.links(rel, ids))
}

func (w *wikiWriter) fileList(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, `<code>`+esc(f)+`</code>`)
	}
	return out
}

func (w *wikiWriter) commands(tests []string) string {
	names := []string{}
	for _, id := range tests {
		if n := w.ix.Node(id); n != nil {
			if f := fileOf(n); f != "" {
				names = append(names, f)
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	plan := runners.Plan(w.g.Root, names)
	if len(plan.Commands) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<ul class="run">` + "\n")
	for _, c := range plan.Commands {
		b.WriteString(`<li><code>` + esc(c) + `</code></li>` + "\n")
	}
	b.WriteString("</ul>\n")
	return b.String()
}

func (w *wikiWriter) indexPage() string {
	rel := "index.html"
	eps := w.ix.OfKind(KindEndpoint)
	pages := w.ix.OfKind(KindPage)
	mods := w.ix.OfKind(KindModule)
	tables := w.ix.OfKind(KindTable)
	changes := w.ix.OfKind(KindChange)

	broken := 0
	for _, e := range w.g.Edges {
		if e.Kind == "calls_missing" {
			broken++
		}
	}
	gate := "The contract gate passes: every frontend call matches an endpoint."
	if broken > 0 {
		gate = plural(broken, "broken call") + " — <code>weave check</code> fails."
	}

	untested := []string{}
	for _, e := range eps {
		if !w.tested(e.ID) {
			untested = append(untested, e.ID)
		}
	}
	noE2E := []string{}
	for _, p := range pages {
		if !w.hasE2E(p.ID) {
			noE2E = append(noE2E, p.ID)
		}
	}

	body := `<h1>` + esc(w.repo) + `</h1>` + "\n<p class=\"lede\">" + gate + "</p>\n"
	body += `<p class="counts">` + strconv.Itoa(len(pages)) + ` screens · ` + strconv.Itoa(len(eps)) +
		` endpoints · ` + strconv.Itoa(len(mods)) + ` modules · ` + strconv.Itoa(len(tables)) +
		` tables · ` + strconv.Itoa(len(changes)) + ` change records</p>` + "\n"

	body += w.section("Where to start", `<ul class="doors">
<li><a href="`+w.href(rel, "index.html")+`#gaps">Gaps in proof</a> — what no test reaches, by the number of screens that call it</li>
<li><a href="`+w.href(rel, "index.html")+`#changes">Changes</a> — what is open, and each change's next task</li>
<li><a href="`+w.href(rel, "index.html")+`#modules">Modules</a> — the backend folders that handle endpoints</li>
</ul>`)

	gapBody := list(append(w.links(rel, untested), w.links(rel, noE2E)...))
	if len(untested) == 0 && len(noE2E) == 0 {
		gapBody = emptyState("Nothing: every endpoint has a direct test and every screen has an end-to-end test.")
	}
	gapSummary := plural(len(untested), "endpoint") + " with no direct test, " + plural(len(noE2E), "screen") + " with no end-to-end test"
	body += `<a id="gaps"></a>` + w.section("Gaps in proof", folded(gapSummary, gapBody))

	var enter [][]string
	for _, e := range eps {
		proof := `<span class="no">no direct test</span>`
		if w.tested(e.ID) {
			proof = `<span class="yes">tested</span>`
		}
		enter = append(enter, []string{w.link(rel, e.ID), proof})
	}
	sort.SliceStable(enter, func(i, j int) bool { return enter[i][0] < enter[j][0] })
	body += `<a id="modules"></a>` + w.section("Modules", folded(plural(len(mods), "module"), w.countList(rel, mods)))
	body += `<a id="changes"></a>` + w.section("Changes", folded(plural(len(changes), "change record"), w.countList(rel, changes)))
	body += `<a id="endpoints"></a>` + w.section("Endpoints", folded(plural(len(eps), "endpoint"), w.tableRows([]string{"Endpoint", "Proof"}, enter)))
	body += `<a id="screens"></a>` + w.section("Screens", folded(plural(len(pages), "screen"), w.countList(rel, pages)))
	body += `<a id="tables"></a>` + w.section("Tables", folded(plural(len(tables), "table"), w.countList(rel, tables)))
	body += w.section("Decisions", w.allNotes(rel))
	return w.chrome(rel, w.repo, "Wiki", body)
}

func (w *wikiWriter) countList(rel string, nodes []*GNode) string {
	items := []string{}
	for _, n := range nodes {
		items = append(items, w.link(rel, n.ID))
	}
	sort.Strings(items)
	return list(items)
}

func (w *wikiWriter) allNotes(rel string) string {
	notes, err := ReadNotes(w.g.Root)
	if err != nil || len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<ul class="notes">` + "\n")
	for _, n := range notes {
		b.WriteString("<li><span class=\"when\">" + esc(n.At[:min(10, len(n.At))]) + "</span> " + esc(n.Note) +
			` <span class="files">` + esc(strings.Join(n.Files, ", ")) + "</span></li>\n")
	}
	b.WriteString("</ul>\n")
	return b.String()
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
