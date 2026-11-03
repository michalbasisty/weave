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

// The wiki is the readable half of the graph: one static page per module, endpoint,
// screen, table and change, generated from the same index the gate trusts. It is
// read-only by construction — regenerating is the only way to change it — and every
// page carries the commit it was built from, so a page can be checked for staleness
// instead of quietly rotting.

// WikiOptions is where and from what the site is written.
type WikiOptions struct {
	OutDir string
	Commit string // full sha, or "" outside git
	Ref    string // short label for the footer
}

// WikiResult is what was written.
type WikiResult struct {
	Pages int
	Index string
}

type wikiWriter struct {
	ix    *Index
	g     *Graph
	opt   WikiOptions
	repo  string
	pages []string // relative paths, for the index
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
	if err := os.WriteFile(filepath.Join(opt.OutDir, "wiki.css"), []byte(wikiCSS), 0o644); err != nil {
		return nil, err
	}

	for _, n := range w.ix.OfKind(KindModule) {
		w.write("m/"+Slug(n.Name)+".html", w.modulePage(n))
	}
	for _, n := range w.ix.OfKind(KindEndpoint) {
		w.write("e/"+Slug(n.Name)+".html", w.endpointPage(n))
	}
	for _, n := range w.ix.OfKind(KindPage) {
		w.write("s/"+Slug(n.Name)+".html", w.screenPage(n))
	}
	for _, n := range w.ix.OfKind(KindTable) {
		w.write("t/"+Slug(n.Name)+".html", w.tablePage(n))
	}
	for _, n := range w.ix.OfKind(KindChange) {
		w.write("c/"+Slug(n.Name)+".html", w.changePage(n))
	}
	w.write("index.html", w.indexPage())
	if err := w.writeStamp(); err != nil {
		return nil, err
	}
	return &WikiResult{Pages: len(w.pages) + 1, Index: filepath.Join(opt.OutDir, "index.html")}, nil
}

// wikiStamp is how a page set proves which commit it describes, so a stale site is
// detected instead of trusted.
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

func slug(s string) string { return Slug(s) }

// href is a page path relative to the page being written.
func (w *wikiWriter) href(from, to string) string {
	depth := strings.Count(from, "/")
	return strings.Repeat("../", depth) + to
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
		// A model is the code-side name of a table; its page is the table's.
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
		return `<span class="missing">` + esc(strings.TrimPrefix(id, "file:")) + `</span>`
	}
	u := w.url(from, n.Kind, id)
	label := n.Name
	if n.Kind == KindModel {
		// Show the table a model maps to, which is what a reader can look up.
		for _, e := range w.ix.From(id, "maps_to") {
			if t := w.ix.Node(e.To); t != nil && t.Kind == KindTable {
				label = t.Name
			}
		}
	}
	if u == "" {
		return esc(label)
	}
	return `<a href="` + u + `">` + esc(label) + `</a>`
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

// chrome wraps a page in the same shell everywhere: signboard, breadcrumb, footer.
func (w *wikiWriter) chrome(rel, title, crumbs, main string) string {
	home := w.href(rel, "index.html")
	css := w.href(rel, "wiki.css")
	stamp := "no git history"
	if w.opt.Ref != "" {
		stamp = "built from " + w.opt.Ref
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
<header class="bar"><a class="brand" href="` + home + `">weave</a><span class="repo">` + esc(w.repo) + `</span><span class="stamp">` + esc(stamp) + `</span></header>
<nav class="crumbs"><a href="` + home + `">` + esc(w.repo) + `</a> › ` + crumbs + `</nav>
<main>
` + main + `
</main>
<footer>
<p>Generated by weave from the same index the contract gate reads. Do not edit: run <code>weave wiki</code>.</p>
<p>` + esc(w.stampLine()) + `</p>
</footer>
</body>
</html>
`
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

// section is a heading plus body, skipped when the body is empty.
func section(title, body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	return "<section>\n<h2>" + esc(title) + "</h2>\n" + body + "\n</section>\n"
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

// emptyState says why a section has nothing, and what would fill it.
func emptyState(why string) string { return `<p class="empty">` + esc(why) + `</p>` }

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

// folded keeps the home page scannable: a long list is one line until opened.
func folded(summary, body string) string {
	return `<details class="fold"><summary>` + esc(summary) + `</summary>` + "\n" + body + "</details>\n"
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

	fields := strconv.Itoa(len(screenIDs)) + " screens"
	if len(screenIDs) == 1 {
		fields = "1 screen"
	}
	lede := "An HTTP endpoint"
	if len(moduleIDs) > 0 {
		lede += " handled in " + w.link(rel, moduleIDs[0])
	}
	lede += ", called from " + fields + ". "
	if w.tested(n.ID) {
		lede += "A test calls it directly."
	} else {
		lede += "No test calls it directly."
	}

	src := ""
	if n.File != nil {
		src = *n.File
		if n.Line != nil && *n.Line > 0 {
			src += ":" + strconv.Itoa(*n.Line)
		}
	}
	body := `<h1>` + esc(n.Name) + `</h1>` + "\n<p class=\"lede\">" + lede + "</p>\n"
	body += section("Source", `<p class="path"><code>`+esc(src)+`</code></p>`)
	body += section("Called from", list(w.links(rel, screenIDs)))
	if len(screenIDs) == 0 {
		body += section("Called from", emptyState("No screen calls this endpoint directly. A mobile client, a service worker or an agent may still use it."))
	}
	proof := list(w.links(rel, tests))
	if len(tests) == 0 {
		proof = emptyState("No test reaches this directly, so a change here is unproven. Give the module a test that calls it, or accept the gap knowingly.")
	}
	body += section("Proved by", proof)
	if cmds := w.commands(tests); cmds != "" {
		body += section("Run the proof", cmds)
	}
	reach := []string{}
	for _, m := range moduleIDs {
		reach = append(reach, w.links(rel, w.targetIDs(m, "uses"))...)
	}
	body += section("Reaches", list(reach))
	body += section("Decisions", w.notes(n.ID))
	body += section("Planned in", w.plannedIn(rel, w.nodeFiles(n.ID)))

	return w.chrome(rel, n.Name, `<a href="`+w.href(rel, "index.html")+`">Endpoints</a> › `+esc(n.Name), body)
}

func (w *wikiWriter) modulePage(n *GNode) string {
	rel := "m/" + Slug(n.Name) + ".html"
	eps := w.targetIDs(n.ID, "contains")
	tables := w.targetIDs(n.ID, "uses")
	files := w.targetIDs(n.ID, "contains")
	if len(eps) == 0 {
		for _, e := range w.ix.To(ModuleID(n.Name), "in_module") {
			eps = append(eps, e.From)
		}
	}
	_ = files
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
			ep := w.ix.Node(id)
			if ep == nil {
				continue
			}
			proof := `<span class="no">no test</span>`
			if w.tested(id) {
				proof = `<span class="yes">tested</span>`
			}
			rows = append(rows, []string{w.link(rel, id), proof})
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
		body += section("Endpoints it handles", w.tableRows([]string{"Endpoint", "Proof"}, rows))
	}
	body += section("Tables it uses", list(w.links(rel, tables)))
	body += section("Files", list(w.fileList(owned)))
	body += section("Tests", list(w.links(rel, tests)))
	if cmds := w.commands(tests); cmds != "" {
		body += section("Run the proof", cmds)
	}
	body += section("Decisions", w.notes(n.ID))
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
		body += section("Rendered by", `<p class="path"><code>`+esc(*n.File)+`</code></p>`)
	}
	body += section("Calls", list(w.links(rel, eps)))
	body += section("Reaches", list(w.links(rel, tables)))
	body += section("Driven by", list(w.links(rel, tests)))
	body += section("Decisions", w.notes(n.ID))
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
	body += section("Used by", list(w.links(rel, mods)))
	body += section("Reachable from", list(w.links(rel, eps)))
	body += section("Decisions", w.notes(n.ID))
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
		title := metaText(t, "title")
		line := `<code>` + esc(id) + `</code> ` + mdInline(title)
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
	body += section("Open tasks", list(openItems))
	body += section("Done", list(doneItems))
	body += section("Requirements", list(w.links(rel, reqIDs)))
	body += section("Files it names", list(uniqueSorted(files)))
	body += section("Decisions", w.notes(n.ID))
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

	body += section("Where to start", `<ul class="doors">
<li><a href="`+w.href(rel, "index.html")+`#modules">Modules</a> — the backend folders that handle endpoints</li>
<li><a href="`+w.href(rel, "index.html")+`#endpoints">Endpoints</a> — every HTTP route, with its proof</li>
<li><a href="`+w.href(rel, "index.html")+`#screens">Screens</a> — every route a person can open</li>
<li><a href="`+w.href(rel, "index.html")+`#gaps">Gaps in proof</a> — what no test reaches</li>
</ul>`)

	var enter [][]string
	for _, e := range eps {
		proof := `<span class="no">no direct test</span>`
		if w.tested(e.ID) {
			proof = `<span class="yes">tested</span>`
		}
		enter = append(enter, []string{w.link(rel, e.ID), proof})
	}
	sort.SliceStable(enter, func(i, j int) bool { return enter[i][0] < enter[j][0] })
	gapBody := list(append(w.links(rel, untested), w.links(rel, noE2E)...))
	if len(untested) == 0 && len(noE2E) == 0 {
		gapBody = emptyState("Nothing: every endpoint has a direct test and every screen has an end-to-end test.")
	}
	gapSummary := plural(len(untested), "endpoint") + " with no direct test, " + plural(len(noE2E), "screen") + " with no end-to-end test"
	body += `<a id="gaps"></a>` + section("Gaps in proof", folded(gapSummary, gapBody))
	body += `<a id="modules"></a>` + section("Modules", folded(plural(len(mods), "module"), w.countList(rel, mods)))
	body += `<a id="endpoints"></a>` + section("Endpoints", folded(plural(len(eps), "endpoint"), w.tableRows([]string{"Endpoint", "Proof"}, enter)))
	body += `<a id="screens"></a>` + section("Screens", folded(plural(len(pages), "screen"), w.countList(rel, pages)))
	body += `<a id="tables"></a>` + section("Tables", folded(plural(len(tables), "table"), w.countList(rel, tables)))
	body += `<a id="changes"></a>` + section("Changes", folded(plural(len(changes), "change record"), w.countList(rel, changes)))
	body += section("Decisions", w.allNotes(rel))
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
		files := strings.Join(n.Files, ", ")
		b.WriteString("<li><span class=\"when\">" + esc(n.At[:min(10, len(n.At))]) + "</span> " + esc(n.Note) +
			` <span class="files">` + esc(files) + "</span></li>\n")
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
