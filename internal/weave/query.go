package weave

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
	"github.com/michalbasisty/weave/internal/junit"
	"github.com/michalbasisty/weave/internal/runners"
)

// A code window is opt-in and never a whole file: dotfiles, keys and anything that
// looks like a secret are refused even when asked for. The TypeScript pattern is
// all ASCII under /i, so it is matched here against an ASCII-lowercased path; `.`
// is spelled out because JS `.` also stops at \r, U+2028 and U+2029.
var reDeny = regexp.MustCompile(`(^|/)(\.env(\.[^\n\r\x{2028}\x{2029}]*)?|id_rsa[^/]*|[^/]*\.(pem|key|p12|pfx)|[^/]*secret[^/]*)$`)

const maxRows = 8

var (
	reModulesSeg = regexp.MustCompile(`modules/([^/]+)/`)
	rePageSep    = regexp.MustCompile(`[/:]`)
)

// Index is the graph with its nodes by id and its edges by endpoint.
type Index struct {
	G    *Graph
	byID map[string]*GNode
	out  map[string][]GEdge
	inn  map[string][]GEdge
}

// NewIndex indexes g. A repeated node id keeps the last node, as Map.set does.
func NewIndex(g *Graph) *Index {
	ix := &Index{G: g, byID: make(map[string]*GNode, len(g.Nodes)), out: map[string][]GEdge{}, inn: map[string][]GEdge{}}
	for _, n := range g.Nodes {
		ix.byID[n.ID] = n
	}
	for _, e := range g.Edges {
		ix.out[e.From] = append(ix.out[e.From], e)
		ix.inn[e.To] = append(ix.inn[e.To], e)
	}
	return ix
}

// Node is the node with id, or nil.
func (ix *Index) Node(id string) *GNode { return ix.byID[id] }

// Has reports whether a node with id exists.
func (ix *Index) Has(id string) bool { _, ok := ix.byID[id]; return ok }

// From is the edges leaving id, of kind when kind is not "".
func (ix *Index) From(id, kind string) []GEdge { return ofEdgeKind(ix.out[id], kind) }

// To is the edges reaching id, of kind when kind is not "".
func (ix *Index) To(id, kind string) []GEdge { return ofEdgeKind(ix.inn[id], kind) }

// OfKind is the nodes of kind, in graph order.
func (ix *Index) OfKind(kind string) []*GNode {
	out := []*GNode{}
	for _, n := range ix.G.Nodes {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

func ofEdgeKind(all []GEdge, kind string) []GEdge {
	if kind == "" {
		return all
	}
	out := []GEdge{}
	for _, e := range all {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// targets is the existing nodes at the end of id's edges of kind.
func (ix *Index) targets(id, kind string) []*GNode {
	out := []*GNode{}
	for _, e := range ix.From(id, kind) {
		if n := ix.Node(e.To); n != nil {
			out = append(out, n)
		}
	}
	return out
}

// nameOr is `n?.name ?? fallback`.
func nameOr(n *GNode, fallback string) string {
	if n == nil {
		return fallback
	}
	return n.Name
}

// fileOf is n?.file, "" when absent: every caller tests it for truthiness.
func fileOf(n *GNode) string {
	if n == nil || n.File == nil {
		return ""
	}
	return *n.File
}

func lineOf(n *GNode) int {
	if n == nil || n.Line == nil {
		return 0
	}
	return *n.Line
}

func metaGet(n *GNode, k string) (jsjson.Value, bool) {
	if n == nil {
		return jsjson.Value{}, false
	}
	return n.Meta.Get(k)
}

// metaTruthy is `!!n?.meta?.[k]`.
func metaTruthy(n *GNode, k string) bool { return truthy(metaGet(n, k)) }

// metaIsTrue is `n?.meta?.[k] === true`.
func metaIsTrue(n *GNode, k string) bool {
	v, ok := metaGet(n, k)
	return ok && v.Kind() == jsjson.KindBool && v.Bool()
}

// metaText is `String(n?.meta?.[k] ?? "")`.
func metaText(n *GNode, k string) string {
	v, ok := metaGet(n, k)
	if !ok || v.IsNull() {
		return ""
	}
	return jsString(v)
}

// jsString is String(v) for a JSON value.
func jsString(v jsjson.Value) string {
	switch v.Kind() {
	case jsjson.KindString:
		return v.Str()
	case jsjson.KindNumber:
		return jsjson.FormatNumber(v.Float())
	case jsjson.KindBool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case jsjson.KindArray:
		parts := make([]string, len(v.Array()))
		for i, x := range v.Array() {
			if !x.IsNull() {
				parts[i] = jsString(x)
			}
		}
		return strings.Join(parts, ",")
	case jsjson.KindObject:
		return "[object Object]"
	}
	return "null"
}

// head is list.slice(0, n).
func head[T any](list []T, n int) []T {
	if n < 0 {
		n += len(list)
		if n < 0 {
			n = 0
		}
	}
	if n > len(list) {
		n = len(list)
	}
	return list[:n]
}

// lastWord is s.split(' ').pop().
func lastWord(s string) string { return s[strings.LastIndexByte(s, ' ')+1:] }

// dirPart is p.slice(0, p.lastIndexOf('/')): with no slash, -1 drops the last character.
func dirPart(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return jsstr.Slice(p, 0, -1)
}

// ordSet is a JS Set of strings: insertion order, delete then add moves to the end.
type ordSet struct {
	list []string
	in   map[string]bool
}

func newSet() *ordSet { return &ordSet{list: []string{}, in: map[string]bool{}} }

func (s *ordSet) add(v string) {
	if !s.in[v] {
		s.in[v] = true
		s.list = append(s.list, v)
	}
}

func (s *ordSet) has(v string) bool { return s.in[v] }
func (s *ordSet) size() int         { return len(s.list) }

func (s *ordSet) del(v string) {
	if !s.in[v] {
		return
	}
	delete(s.in, v)
	for i, x := range s.list {
		if x == v {
			s.list = append(s.list[:i:i], s.list[i+1:]...)
			return
		}
	}
}

func (s *ordSet) items() []string { return append([]string{}, s.list...) }

func (s *ordSet) clone() *ordSet {
	c := newSet()
	for _, v := range s.list {
		c.add(v)
	}
	return c
}

// ---------------------------------------------------------------- analysis

// PageHit is a page a change reaches, and how sure the link is.
type PageHit struct {
	Name string `json:"name"`
	Conf string `json:"conf"`
}

// ChangeHit is a spec change record whose tasks name a touched file.
type ChangeHit struct {
	Slug     string   `json:"slug"`
	Done     int      `json:"done"`
	Total    int      `json:"total"`
	Open     []string `json:"open"`
	File     string   `json:"file"`
	Evidence bool     `json:"evidence"`
	Score    int      `json:"score"`
}

// Facts is everything a change to some files can reach.
type Facts struct {
	Files         []string    `json:"files"`
	Dependents    []string    `json:"dependents"`
	Label         string      `json:"label"`
	Modules       []string    `json:"modules"`
	Endpoints     []*GNode    `json:"endpoints"`
	Pages         []PageHit   `json:"pages"`
	Tables        []string    `json:"tables"`
	Tests         []string    `json:"tests"`
	Commands      []string    `json:"commands"`
	Reqs          []string    `json:"reqs"`
	Suspect       []string    `json:"suspect"`
	Orphans       []string    `json:"orphans"`
	Untested      []string    `json:"untested"`
	Unclaimed     []string    `json:"unclaimed"`
	SchemaChanged bool        `json:"schemaChanged"`
	Changes       []ChangeHit `json:"changes"`
}

// pageMap is the Map<name, conf>: set on a present key keeps its place.
type pageMap struct {
	list []PageHit
	at   map[string]int
}

func (p *pageMap) set(name, conf string) {
	if i, ok := p.at[name]; ok {
		p.list[i].Conf = conf
		return
	}
	p.at[name] = len(p.list)
	p.list = append(p.list, PageHit{name, conf})
}

func (p *pageMap) get(name string) string {
	if i, ok := p.at[name]; ok {
		return p.list[i].Conf
	}
	return ""
}

func (p *pageMap) has(name string) bool { _, ok := p.at[name]; return ok }

// Analyze is everything a change to inputs can reach. Nil when no file matched.
func Analyze(g *Graph, inputs []string) *Facts {
	ix := NewIndex(g)
	root := strings.TrimSuffix(strings.ReplaceAll(g.Root, `\`, "/"), "/") + "/"
	files := newSet()
	for _, raw := range inputs {
		p := jsstr.Trim(strings.ReplaceAll(raw, `\`, "/"))
		if strings.HasPrefix(p, root) {
			p = p[len(root):]
		}
		p = strings.TrimPrefix(p, "./")
		if ix.Has(FileID(p)) {
			files.add(p)
			continue
		}
		for _, n := range ix.OfKind(KindFile) {
			if n.Name == p || strings.HasSuffix(n.Name, "/"+p) || strings.Contains(n.Name, p) {
				files.add(n.Name)
			}
		}
	}
	if files.size() == 0 {
		return nil
	}

	// Direct importers. One hop only: a shared util's full transitive tree is noise,
	// but "who imports this" is the answer an agent needs before touching it.
	dependents := newSet()
	for _, f := range files.list {
		for _, e := range ix.To(FileID(f), "imports") {
			if n := ix.Node(e.From); fileOf(n) != "" && !files.has(*n.File) {
				dependents.add(*n.File)
			}
		}
	}

	modules := newSet()
	for _, f := range files.list {
		if m := reModulesSeg.FindStringSubmatch(f); m != nil {
			modules.add(m[1])
		}
	}
	ownModules := modules.clone()
	// A widely imported file touches every importer's module: only expand when that stays readable.
	if dependents.size() <= 12 {
		for _, f := range dependents.list {
			if m := reModulesSeg.FindStringSubmatch(f); m != nil {
				modules.add(m[1])
			}
		}
	}
	for _, m := range modules.items() {
		if !ix.Has(ModuleID(m)) {
			modules.del(m)
		}
	}

	// Seed endpoints: defined in a changed file or module, or called by one.
	endpoints := newSet()
	for _, e := range ix.OfKind(KindEndpoint) {
		if fileOf(e) != "" && files.has(*e.File) {
			endpoints.add(e.ID)
		} else if v, ok := metaGet(e, "module"); truthy(v, ok) && modules.has(jsString(v)) {
			endpoints.add(e.ID)
		}
	}
	for _, f := range files.list {
		for _, e := range ix.From(FileID(f), "calls") {
			endpoints.add(e.To)
		}
		for _, r := range ix.To(FileID(f), "renders") {
			for _, c := range ix.From(r.From, "calls") {
				endpoints.add(c.To)
			}
		}
	}
	for _, ep := range endpoints.list {
		if v, ok := metaGet(ix.Node(ep), "module"); ok && v.Kind() == jsjson.KindString && ix.Has(ModuleID(v.Str())) {
			modules.add(v.Str())
		}
	}

	pages := &pageMap{at: map[string]int{}}
	for _, f := range files.list {
		for _, kind := range []string{"renders", "declared_in"} {
			for _, r := range ix.To(FileID(f), kind) {
				if n := ix.Node(r.From); n != nil && n.Kind == KindPage {
					pages.set(n.Name, ConfExact)
				}
			}
		}
	}
	for _, ep := range endpoints.list {
		for _, e := range ix.To(ep, "calls") {
			n := ix.Node(e.From)
			if n == nil || n.Kind != KindPage {
				continue
			}
			if pages.get(n.Name) != ConfExact {
				pages.set(n.Name, e.Conf)
			}
		}
	}

	schemaChanged := false
	for _, f := range files.list {
		if strings.HasSuffix(f, ".prisma") {
			schemaChanged = true
			break
		}
	}
	models := newSet()
	if schemaChanged {
		for _, m := range ix.OfKind(KindModel) {
			models.add(m.ID)
		}
	}
	for _, m := range modules.list {
		for _, e := range ix.From(ModuleID(m), "uses") {
			models.add(e.To)
		}
	}
	tables := newSet()
	for _, m := range models.list {
		for _, e := range ix.From(m, "maps_to") {
			tables.add(nameOr(ix.Node(e.To), e.To))
		}
	}

	tests := newSet()
	for _, t := range ix.OfKind(KindTest) {
		if fileOf(t) != "" && files.has(*t.File) {
			tests.add(t.Name)
		}
		for _, e := range ix.From(t.ID, "") {
			if endpoints.has(e.To) {
				tests.add(t.Name)
			}
			if modules.has(strings.Replace(e.To, "module:", "", 1)) {
				tests.add(t.Name)
			}
		}
		for _, e := range ix.From(t.ID, "exercises") {
			if pages.has(nameOr(ix.Node(e.To), "")) {
				tests.add(t.Name)
			}
		}
	}

	reqs := newSet()
	for _, f := range files.list {
		// a @req marker in the code is the strongest link there is
		for _, e := range ix.From(FileID(f), "implements") {
			reqs.add(nameOr(ix.Node(e.To), e.To))
		}
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		for _, e := range ix.To(FileID(f), "specified_in") {
			reqs.add(nameOr(ix.Node(e.From), e.From))
		}
	}

	// Call sites in the changed files that match no endpoint: a contract already broken.
	orphans := newSet()
	for _, f := range files.list {
		for _, e := range ix.From(FileID(f), "calls_missing") {
			orphans.add(nameOr(ix.Node(e.To), e.To))
		}
	}

	// Spec change records whose tasks name any touched file. Ranked: a change that
	// mentions the module/page wins over one that merely touched the same file.
	changes := []ChangeHit{}
	keywords := []string{}
	for _, k := range modules.list {
		if k != "" {
			keywords = append(keywords, strings.ToLower(k))
		}
	}
	for _, p := range pages.list {
		if k := jsstr.Trim(rePageSep.ReplaceAllString(p.Name, " ")); k != "" {
			keywords = append(keywords, strings.ToLower(k))
		}
	}
	touches := func(t *GNode, exactOnly bool) bool {
		for _, e := range ix.From(t.ID, "touches") {
			if (!exactOnly || e.Conf == ConfExact) && files.has(nameOr(ix.Node(e.To), "")) {
				return true
			}
		}
		return false
	}
	for _, c := range ix.OfKind(KindChange) {
		tasks := ix.targets(c.ID, "has_task")
		if len(tasks) == 0 {
			continue
		}
		var exactHit, hit []*GNode
		for _, t := range tasks {
			if touches(t, true) {
				exactHit = append(exactHit, t)
			}
		}
		hit = exactHit
		if len(exactHit) == 0 {
			for _, t := range tasks {
				if touches(t, false) {
					hit = append(hit, t)
				}
			}
		}
		if len(hit) == 0 {
			continue
		}
		for _, e := range ix.From(hit[0].ID, "satisfies") {
			reqs.add(nameOr(ix.Node(e.To), e.To))
		}
		done := 0
		open := []string{}
		for _, t := range tasks {
			if metaTruthy(t, "done") {
				done++
			} else {
				open = append(open, jsstr.Trim(lastWord(t.Name)+" "+metaText(t, "title")))
			}
		}
		slug := strings.ToLower(c.Name)
		// A task that names the file itself beats one that only names its directory.
		score := len(hit) * 10
		if len(exactHit) > 0 {
			score += 15
		}
		if len(hit) == len(tasks) {
			score += 5
		}
		for _, k := range keywords {
			if jsstr.Len(k) > 3 && strings.Contains(slug, k) {
				score += 25
				break
			}
		}
		changes = append(changes, ChangeHit{Slug: c.Name, Done: done, Total: len(tasks), Open: open, File: fileOf(c), Evidence: len(hit) == len(tasks), Score: score})
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Score > changes[j].Score })

	untested, unclaimed := []string{}, []string{}
	for _, ep := range endpoints.list {
		if len(ix.To(ep, "hits")) == 0 && len(ix.To(ep, "covers")) == 0 {
			untested = append(untested, nameOr(ix.Node(ep), ep))
		}
		claimed := false
		for _, e := range ix.To(ep, "calls") {
			if n := ix.Node(e.From); n != nil && n.Kind == KindPage {
				claimed = true
				break
			}
		}
		if !claimed {
			unclaimed = append(unclaimed, nameOr(ix.Node(ep), ep))
		}
	}

	// Requirements whose text changed since the index the code was written against.
	suspect := []string{}
	for _, r := range reqs.list {
		if metaIsTrue(ix.Node(ReqID(r)), "suspect") {
			suspect = append(suspect, r)
		}
	}

	commands := runners.Plan(g.Root, tests.items()).Commands

	pageDirs := map[string]bool{}
	for _, p := range ix.OfKind(KindPage) {
		if fileOf(p) != "" {
			pageDirs[dirPart(*p.File)] = true
		}
	}
	isPageFile := false
	for _, f := range files.list {
		if len(ix.To(FileID(f), "renders")) > 0 || pageDirs[dirPart(f)] {
			isPageFile = true
			break
		}
	}
	label := files.list[0][strings.LastIndexByte(files.list[0], '/')+1:]
	if ownModules.size() > 0 {
		label = "module " + ownModules.list[0]
	}
	if isPageFile && len(pages.list) > 0 {
		label = pages.list[0].Name
	}

	eps := make([]*GNode, 0, endpoints.size())
	for _, id := range endpoints.list {
		n := ix.Node(id)
		if n == nil {
			// The TypeScript would hand back undefined and fail on its name; keep the id.
			n = &GNode{ID: id, Kind: KindEndpoint}
		}
		eps = append(eps, n)
	}

	return &Facts{
		Files:         files.items(),
		Dependents:    dependents.items(),
		Label:         label,
		Modules:       modules.items(),
		Endpoints:     eps,
		Pages:         append([]PageHit{}, pages.list...),
		Tables:        tables.items(),
		Tests:         tests.items(),
		Commands:      commands,
		Reqs:          reqs.items(),
		Suspect:       suspect,
		Orphans:       orphans.items(),
		Untested:      untested,
		Unclaimed:     unclaimed,
		SchemaChanged: schemaChanged,
		Changes:       changes,
	}
}

func loc(n *GNode) string {
	file := fileOf(n)
	if file == "" {
		return ""
	}
	parts := strings.Split(file, "/")
	short := strings.Join(parts[max(0, len(parts)-2):], "/")
	if l := lineOf(n); l != 0 {
		return short + ":" + strconv.Itoa(l)
	}
	return short
}

func indent(prefix string, list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = prefix + s
	}
	return out
}

// ---------------------------------------------------------------- impact

// Impact is the full blast radius of a change to inputs.
func Impact(g *Graph, inputs []string) string {
	f := Analyze(g, inputs)
	if f == nil {
		return "no file matched: " + strings.Join(inputs, ", ")
	}
	out := []string{"impact: " + strconv.Itoa(len(f.Files)) + " file(s)"}
	out = append(out, indent("  ", head(f.Files, maxRows))...)
	if len(f.Dependents) > 0 {
		out = append(out, "used by   "+strconv.Itoa(len(f.Dependents))+" file(s)")
		out = append(out, indent("  ", head(f.Dependents, 5))...)
	}
	if len(f.Modules) > 0 {
		out = append(out, "modules   "+strings.Join(f.Modules, ", "))
	}
	out = append(out, "endpoints "+strconv.Itoa(len(f.Endpoints)))
	for _, e := range head(f.Endpoints, maxRows) {
		out = append(out, "  "+e.Name+"  "+loc(e))
	}
	if f.SchemaChanged {
		out = append(out, "tables    all "+strconv.Itoa(len(f.Tables))+" (schema changed)")
	} else if len(f.Tables) > 0 {
		out = append(out, "tables    "+strings.Join(f.Tables, ", "))
	}
	out = append(out, "pages     "+strconv.Itoa(len(f.Pages)))
	for _, p := range head(f.Pages, maxRows) {
		out = append(out, "  "+p.Name+" ("+p.Conf+")")
	}
	out = append(out, "tests     "+strconv.Itoa(len(f.Tests)))
	out = append(out, indent("  ", head(f.Tests, maxRows))...)
	if len(f.Reqs) > 0 {
		out = append(out, "reqs      "+strings.Join(f.Reqs, ", "))
	}
	for _, c := range head(f.Changes, 2) {
		line := "spec      " + c.Slug + " " + strconv.Itoa(c.Done) + "/" + strconv.Itoa(c.Total)
		if len(c.Open) > 0 {
			line += ", open: " + c.Open[0]
		}
		out = append(out, line)
	}
	if len(f.Untested) > 0 {
		out = append(out, "untested  "+strconv.Itoa(len(f.Untested))+" endpoint(s) with no direct test")
		out = append(out, indent("  ", head(f.Untested, 4))...)
	}
	if len(f.Unclaimed) > 0 {
		out = append(out, "unclaimed "+strconv.Itoa(len(f.Unclaimed))+" endpoint(s) no page calls")
		out = append(out, indent("  ", head(f.Unclaimed, 4))...)
	}
	if len(f.Orphans) > 0 {
		out = append(out, "BROKEN    "+strconv.Itoa(len(f.Orphans))+" call site(s) match no endpoint")
		out = append(out, indent("  ", head(f.Orphans, 4))...)
	}
	if len(f.Suspect) > 0 {
		out = append(out, "suspect   "+strings.Join(f.Suspect, ", ")+" — spec text changed, code not re-checked")
	}
	if len(f.Commands) > 0 {
		out = append(out, "run")
		out = append(out, indent("  ", f.Commands)...)
	}
	if len(f.Endpoints) == 0 && len(f.Pages) == 0 && len(f.Tables) == 0 {
		out = append(out, "no endpoint, page or table reaches this file — change looks isolated")
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------- context pack

// Context is the one call an agent should make before editing: what this file is,
// what serves it, which spec owns it, which tests to run, and what else moves. Hard
// budget so the answer stays cheaper than reading the files. depth "code" adds a
// short window at each anchor.
func Context(g *Graph, inputs []string, budgetTokens float64, depth string) string {
	f := Analyze(g, inputs)
	if f == nil {
		return "no file matched: " + strings.Join(inputs, ", ")
	}
	limit := math.Max(400, budgetTokens*4)
	lines := []string{}
	used := 0
	push := func(s string) bool {
		n := jsstr.Len(s)
		if float64(used+n+1) > limit {
			return false
		}
		lines = append(lines, s)
		used += n + 1
		return true
	}

	push(f.Label + "  " + f.Files[0])
	if len(f.Dependents) > 0 {
		s := "used by   " + strconv.Itoa(len(f.Dependents)) + " file(s)"
		if len(f.Dependents) > 12 {
			s += " — wide blast radius"
		}
		push(s)
	}
	if len(f.Pages) > 0 {
		shown := []string{}
		for _, p := range head(f.Pages, 4) {
			if p.Conf == ConfInferred {
				shown = append(shown, p.Name+"~")
			} else {
				shown = append(shown, p.Name)
			}
		}
		s := "page      " + strings.Join(shown, ", ")
		if len(f.Pages) > 4 {
			s += " +" + strconv.Itoa(len(f.Pages)-4)
		}
		push(s)
	}
	if len(f.Modules) > 0 {
		push("module    " + strings.Join(f.Modules, ", "))
	}

	for i, e := range head(f.Endpoints, 4) {
		lead := "endpoint  "
		if i > 0 {
			lead = "          "
		}
		push(lead + e.Name + "  " + loc(e))
	}
	if len(f.Endpoints) > 4 {
		push("          +" + strconv.Itoa(len(f.Endpoints)-4) + " more")
	}

	if len(f.Tables) > 0 {
		s := "tables    " + strings.Join(head(f.Tables, 8), ", ")
		if len(f.Tables) > 8 {
			s += " …"
		}
		push(s)
	}
	if len(f.Reqs) > 0 {
		push("reqs      " + strings.Join(head(f.Reqs, 5), ", "))
	}
	if len(f.Orphans) > 0 {
		s := "BROKEN    " + strings.Join(head(f.Orphans, 3), "; ")
		if len(f.Orphans) > 3 {
			s += " +" + strconv.Itoa(len(f.Orphans)-3)
		}
		push(s + " — matches no endpoint")
	}
	if len(f.Suspect) > 0 {
		push("suspect   " + strings.Join(head(f.Suspect, 3), ", ") + " — spec changed, code not re-checked")
	}

	// Notes are a previous reader's claim, never evidence: one line each, labelled.
	for _, note := range NotesFor(g.Root, inputs) {
		text := jsstr.Slice(note.Note, 0, 120)
		if jsstr.Len(note.Note) > 120 {
			text += "…"
		}
		if !push("note      " + jsstr.Slice(note.At, 0, 10) + "  " + text + " (not evidence)") {
			break
		}
	}

	if len(f.Changes) > 0 {
		ch := f.Changes[0]
		s := "spec      " + ch.Slug + " " + strconv.Itoa(ch.Done) + "/" + strconv.Itoa(ch.Total)
		if !ch.Evidence {
			s += " (partly names this file)"
		}
		push(s)
		if len(ch.Open) > 0 {
			push("open      " + strings.Join(head(ch.Open, 3), "; "))
		}
	}

	if len(f.Tests) > 0 {
		push("tests     " + strconv.Itoa(len(f.Tests)))
		for _, t := range head(f.Tests, 6) {
			if !push("          " + t) {
				break
			}
		}
	} else {
		push("tests     none reference this — write one first (red before green)")
	}
	if len(f.Untested) > 0 {
		s := "untested  " + strings.Join(head(f.Untested, 3), ", ")
		if len(f.Untested) > 3 {
			s += " +" + strconv.Itoa(len(f.Untested)-3)
		}
		push(s)
	}
	if len(f.Endpoints) > 0 && len(f.Pages) > 1 {
		others := []string{}
		for _, p := range head(f.Pages[1:], 4) {
			others = append(others, p.Name)
		}
		push("also      " + strings.Join(others, ", ") + " share these endpoints")
	}
	if len(f.Commands) > 0 {
		push("run")
		for _, c := range f.Commands {
			if !push("  " + c) {
				break
			}
		}
	}

	if depth == "code" {
		pushCode(g, f, push)
	}
	return strings.Join(lines, "\n")
}

type anchor struct {
	file string
	line int
}

// pushCode adds up to three anchors, each a short window. The pack is still capped by push.
func pushCode(g *Graph, f *Facts, push func(string) bool) {
	anchorAt := func(n *GNode) anchor {
		l := lineOf(n)
		if l == 0 {
			l = 1
		}
		return anchor{*n.File, l}
	}
	anchors := []anchor{}
	for _, e := range head(f.Endpoints, 2) {
		if fileOf(e) != "" {
			anchors = append(anchors, anchorAt(e))
		}
	}
	pageNames, tableNames := map[string]bool{}, map[string]bool{}
	for _, p := range f.Pages {
		pageNames[p.Name] = true
	}
	for _, t := range f.Tables {
		tableNames[t] = true
	}
	for _, want := range []struct {
		kind  string
		names map[string]bool
	}{{KindPage, pageNames}, {KindTable, tableNames}} {
		for _, n := range g.Nodes {
			if n.Kind == want.kind && fileOf(n) != "" && want.names[n.Name] {
				anchors = append(anchors, anchorAt(n))
				break
			}
		}
	}

	seen, seenFile := map[string]bool{}, map[string]bool{}
	shown := 0
	for _, a := range anchors {
		if shown >= 3 {
			break
		}
		key := a.file + ":" + strconv.Itoa(a.line)
		if seen[key] || seenFile[a.file] || reDeny.MatchString(asciiLower(a.file)) {
			continue
		}
		seen[key] = true
		seenFile[a.file] = true
		label, body, ok := codeWindow(g.Root, a.file, a.line)
		if !ok {
			continue
		}
		if !push("code      " + label) {
			break
		}
		room := true
		for _, l := range body {
			if !push("  " + l) {
				room = false
				break
			}
		}
		shown++
		if !room {
			break
		}
	}
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// resolveIn is path.resolve(base, p) for an absolute base.
func resolveIn(base, p string) string {
	switch {
	case filepath.IsAbs(p):
		return filepath.Clean(p)
	case p != "" && os.IsPathSeparator(p[0]):
		return filepath.Clean(filepath.VolumeName(base) + p)
	}
	return filepath.Join(base, p)
}

const maxWindowFile = 512 * 1024

func codeWindow(root, file string, line int) (string, []string, bool) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", nil, false
	}
	abs := resolveIn(base, file)
	if !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return "", nil, false // never read outside the repo
	}
	// Every UTF-16 unit takes at most three bytes: a bigger file is over the cap unread.
	if st, err := os.Stat(abs); err != nil || st.Size() > 3*maxWindowFile {
		return "", nil, false
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, false
	}
	text := jsstr.DecodeUTF8(raw)
	if jsstr.Len(text) > maxWindowFile {
		return "", nil, false
	}
	lines := jsstr.SplitLines(text)
	at := min(max(1, line), len(lines))
	start := max(0, at-7)
	end := min(len(lines), at+7)
	body := make([]string, 0, end-start)
	for i, l := range lines[start:end] {
		body = append(body, jsstr.PadStart(strconv.Itoa(start+1+i), 4)+"  "+l)
	}
	return file + ":" + strconv.Itoa(start+1) + "-" + strconv.Itoa(end), body, true
}

// ---------------------------------------------------------------- plan

// Plan is the next unit of work for a spec change, with only what is needed to
// start it: the task, its files, its endpoints, its tests and the command to run
// them. taskID "" picks the first open task.
func Plan(g *Graph, slug, taskID string) string {
	ix := NewIndex(g)
	c := ix.Node(ChangeID(slug))
	if c == nil {
		return `no change "` + slug + `" — try: changes() for the list`
	}
	tasks := ix.targets(c.ID, "has_task")
	if len(tasks) == 0 {
		return slug + ": no tasks"
	}
	var task *GNode
	for _, t := range tasks {
		if (taskID != "" && strings.HasSuffix(t.Name, " "+taskID)) || (taskID == "" && !metaIsTrue(t, "done")) {
			task = t
			break
		}
	}
	if task == nil {
		return slug + ": all " + strconv.Itoa(len(tasks)) + " task(s) done — call verify"
	}

	out := []string{"next  " + lastWord(task.Name) + "  " + metaText(task, "title")}
	names := func(kind string) []string {
		list := []string{}
		for _, e := range ix.From(task.ID, kind) {
			if n := ix.Node(e.To); n != nil && n.Name != "" {
				list = append(list, n.Name)
			}
		}
		return list
	}
	files := names("touches")
	if len(files) > 0 {
		out = append(out, "  files  "+files[0])
		out = append(out, indent("         ", head(files[1:], 5))...)
	}
	if reqs := names("satisfies"); len(reqs) > 0 {
		out = append(out, "  reqs   "+strings.Join(reqs, ", "))
	}

	if len(files) > 0 {
		if f := Analyze(g, files); f != nil {
			if len(f.Endpoints) > 0 {
				api := []string{}
				for _, e := range head(f.Endpoints, 3) {
					api = append(api, e.Name)
				}
				out = append(out, "  api    "+strings.Join(api, ", "))
			}
			if len(f.Tables) > 0 {
				out = append(out, "  tables "+strings.Join(head(f.Tables, 6), ", "))
			}
			if len(f.Tests) > 0 {
				out = append(out, "  tests  "+strconv.Itoa(len(f.Tests)))
			}
			if len(f.Orphans) > 0 {
				out = append(out, "  BROKEN "+strings.Join(head(f.Orphans, 3), ", "))
			}
			if len(f.Suspect) > 0 {
				out = append(out, "  suspect "+strings.Join(head(f.Suspect, 3), ", "))
			}
			out = append(out, indent("  run    ", f.Commands)...)
		}
	}
	done := 0
	for _, t := range tasks {
		if metaIsTrue(t, "done") {
			done++
		}
	}
	out = append(out, "  change "+slug+" "+strconv.Itoa(done)+"/"+strconv.Itoa(len(tasks)))
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------- evidence

// ReqEvidence is what the test reports say about one requirement.
type ReqEvidence struct {
	ID         string   `json:"id"`
	Pass       int      `json:"pass"`
	Fail       int      `json:"fail"`
	Skip       int      `json:"skip"`
	Tests      []string `json:"tests"`
	Unverified bool     `json:"unverified"`
}

// EvidenceFacts joins the graph's requirement→test edges with a JUnit report. This
// is the only claim that cannot be faked by an agent: a named test ran and
// reported a result. A non-empty only keeps those requirement ids.
func EvidenceFacts(g *Graph, cases []junit.Case, only []string) []ReqEvidence {
	ix := NewIndex(g)
	var filter map[string]bool
	if len(only) > 0 {
		filter = map[string]bool{}
		for _, id := range only {
			filter[id] = true
		}
	}
	byReq := map[string]*ReqEvidence{}
	order := []*ReqEvidence{}
	touch := func(id string) *ReqEvidence {
		e := byReq[id]
		if e == nil {
			e = &ReqEvidence{ID: id, Tests: []string{}}
			byReq[id] = e
			order = append(order, e)
		}
		return e
	}

	tests := ix.OfKind(KindTest)
	for _, c := range cases {
		ids := newSet()
		for _, id := range junit.ReqsInCase(c) {
			ids.add(id)
		}
		rel := ""
		if c.File != nil {
			rel = strings.ReplaceAll(*c.File, `\`, "/")
		}
		if rel != "" {
			for _, t := range tests {
				if t.Name == rel || strings.HasSuffix(t.Name, "/"+rel) || strings.HasSuffix(rel, "/"+t.Name) {
					for _, e := range ix.From(t.ID, "") {
						if e.Kind == "verifies" || e.Kind == "asserts" {
							ids.add(strings.TrimPrefix(e.To, "req:"))
						}
					}
					break
				}
			}
		}
		for _, id := range ids.list {
			if filter != nil && !filter[id] {
				continue
			}
			e := touch(id)
			switch c.Status {
			case junit.Pass:
				e.Pass++
			case junit.Fail:
				e.Fail++
			default:
				e.Skip++
			}
			if len(e.Tests) < 3 {
				name := c.Name
				if c.Status != junit.Pass {
					name += " [" + string(c.Status) + "]"
				}
				e.Tests = append(e.Tests, name)
			}
		}
	}

	// Implemented in code but never exercised by a test.
	for _, n := range ix.OfKind(KindReq) {
		id := n.Name
		if filter != nil && !filter[id] {
			continue
		}
		if byReq[id] != nil {
			continue
		}
		if len(ix.To(n.ID, "implements")) > 0 {
			touch(id).Unverified = true
		}
	}

	out := make([]ReqEvidence, len(order))
	for i, e := range order {
		out[i] = *e
	}
	sort.SliceStable(out, func(i, j int) bool { return jsstr.LocaleCompare(out[i].ID, out[j].ID) < 0 })
	return out
}

// Evidence is EvidenceFacts as text, one requirement per line.
func Evidence(g *Graph, cases []junit.Case, only []string) string {
	facts := EvidenceFacts(g, cases, only)
	if len(facts) == 0 {
		return "evidence: no requirement is linked to a test yet — add `@req ID` to a test file"
	}
	verified := 0
	for _, f := range facts {
		if f.Pass > 0 && f.Fail == 0 {
			verified++
		}
	}
	out := []string{"evidence: " + strconv.Itoa(verified) + "/" + strconv.Itoa(len(facts)) + " requirement(s) verified, " + strconv.Itoa(len(cases)) + " test case(s)"}
	for _, f := range facts {
		status := "no test"
		switch {
		case f.Fail > 0:
			status = "FAIL"
		case f.Pass > 0:
			status = "pass"
		case f.Skip > 0:
			status = "skip"
		}
		detail := "implemented via @req, never exercised"
		if len(f.Tests) > 0 {
			detail = f.Tests[0]
		}
		out = append(out, jsstr.PadEnd(f.ID, 16)+" "+jsstr.PadEnd(status, 7)+" "+jsstr.PadStart(strconv.Itoa(f.Pass+f.Fail), 2)+" test(s)  "+detail)
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------- change view

// ListChanges is the newest change records first, with task progress. Capped: an
// agent needs orientation, not 88 rows.
func ListChanges(g *Graph, limit int) string {
	ix := NewIndex(g)
	all := ix.OfKind(KindChange)
	sort.SliceStable(all, func(i, j int) bool { return jsstr.LocaleCompare(all[j].Name, all[i].Name) < 0 })
	if len(all) == 0 {
		return "no change records"
	}
	rows := []string{}
	for _, c := range head(all, limit) {
		tasks := ix.From(c.ID, "has_task")
		done := 0
		for _, e := range tasks {
			if metaIsTrue(ix.Node(e.To), "done") {
				done++
			}
		}
		rows = append(rows, c.Name+" "+strconv.Itoa(done)+"/"+strconv.Itoa(len(tasks)))
	}
	if len(all) > limit {
		rows = append(rows, "… "+strconv.Itoa(len(all)-limit)+" older")
	}
	return strings.Join(rows, "\n")
}

// ChangeView is one change record: its progress, open tasks, files and their impact.
func ChangeView(g *Graph, slug string) string {
	ix := NewIndex(g)
	cid := ChangeID(slug)
	if ix.Node(cid) == nil {
		near := []string{}
		for _, n := range ix.OfKind(KindChange) {
			if strings.Contains(n.Name, slug) {
				near = append(near, n.Name)
			}
		}
		if len(near) > 0 {
			return `no change "` + slug + `". close: ` + strings.Join(head(near, 6), ", ")
		}
		return `no change "` + slug + `"`
	}
	tasks := ix.targets(cid, "has_task")
	files := newSet()
	for _, t := range tasks {
		for _, e := range ix.From(t.ID, "touches") {
			if n := ix.Node(e.To); fileOf(n) != "" {
				files.add(*n.File)
			}
		}
	}
	out := []string{"change " + slug}
	done := 0
	open := []*GNode{}
	for _, t := range tasks {
		if metaTruthy(t, "done") {
			done++
		} else {
			open = append(open, t)
		}
	}
	out = append(out, "tasks "+strconv.Itoa(done)+"/"+strconv.Itoa(len(tasks)))
	for _, t := range head(open, 6) {
		out = append(out, "  open "+lastWord(t.Name)+"  "+metaText(t, "title"))
	}
	out = append(out, indent("  ", head(files.list, maxRows))...)
	if files.size() > 0 {
		out = append(out, "", Impact(g, files.items()))
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------- search

var searchWeight = map[string]int{
	KindEndpoint: 6, KindPage: 5, KindModule: 4, KindModel: 4, KindTable: 2, KindReq: 5, KindTest: 3, KindFile: 1, KindTask: 2, KindChange: 3,
}

// Search ranks named nodes against q, at most three per kind.
func Search(g *Graph, q string, limit int) string {
	needle := strings.ToLower(q)
	type hit struct {
		n *GNode
		s int
	}
	scored := []hit{}
	for _, n := range g.Nodes {
		if n.Kind == KindFile {
			continue
		}
		name := strings.ToLower(n.Name)
		s := 0
		switch {
		case name == needle:
			s = 20
		case strings.HasPrefix(name, needle):
			s = 12
		case strings.Contains(name, needle):
			s = 7
		case strings.Contains(strings.ToLower(fileOf(n)), needle):
			s = 3
		case strings.Contains(strings.ToLower(metaText(n, "title")), needle):
			s = 4
		}
		if s == 0 {
			continue
		}
		scored = append(scored, hit{n, s + searchWeight[n.Kind]})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].s != scored[j].s {
			return scored[i].s > scored[j].s
		}
		return jsstr.LocaleCompare(scored[i].n.Name, scored[j].n.Name) < 0
	})
	if len(scored) == 0 {
		return `nothing matched "` + q + `"`
	}
	perKind := map[string]int{}
	out := []string{}
	for _, h := range scored {
		if perKind[h.n.Kind] >= 3 {
			continue
		}
		perKind[h.n.Kind]++
		line := jsstr.PadEnd(h.n.Kind, 9) + " " + h.n.Name
		if f := fileOf(h.n); f != "" {
			line += "  " + f
		}
		out = append(out, line)
		if len(out) >= limit {
			break
		}
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------- doctor

// jsRound is Math.round: halves go up, toward +Infinity.
func jsRound(v float64) float64 {
	r := math.Floor(v)
	if v-r >= 0.5 {
		r++
	}
	return r
}

// PhaseLine is one line of phase timings, shared by `index`, `perf` and `doctor`.
func PhaseLine(p *Perf, compact bool) string {
	parts := make([]string, len(p.Phases))
	for i, ph := range p.Phases {
		parts[i] = ph.Name + " " + jsjson.FormatNumber(jsRound(ph.Ms)) + "ms"
	}
	if compact {
		return strings.Join(parts, " · ")
	}
	return strings.Join(parts, " · ") + " · heap " + jsjson.FormatNumber(p.HeapMb) + "MB"
}

// Doctor is the index's health: counts, unlinked endpoints and pages, recall, timings.
func Doctor(g *Graph) string {
	ix := NewIndex(g)
	counts := map[string]int{}
	for _, n := range g.Nodes {
		counts[n.Kind]++
	}
	order := []string{KindModule, KindEndpoint, KindModel, KindTable, KindPage, KindTest, KindReq, KindTask, KindChange, KindFile}
	var noPage, noTest []string
	for _, e := range ix.OfKind(KindEndpoint) {
		claimed := false
		for _, x := range ix.To(e.ID, "calls") {
			if n := ix.Node(x.From); n != nil && n.Kind == KindPage {
				claimed = true
				break
			}
		}
		if !claimed {
			noPage = append(noPage, e.Name)
		}
		if len(ix.To(e.ID, "hits")) == 0 && len(ix.To(e.ID, "covers")) == 0 {
			noTest = append(noTest, e.Name)
		}
	}
	var pageNoTest []string
	for _, p := range ix.OfKind(KindPage) {
		if len(ix.To(p.ID, "exercises")) == 0 {
			pageNoTest = append(pageNoTest, p.Name)
		}
	}
	kinds := []string{}
	for _, k := range order {
		if c, ok := counts[k]; ok {
			kinds = append(kinds, k+" "+strconv.Itoa(c))
		}
	}
	lines := []string{"files " + strconv.Itoa(g.FileCount), strings.Join(kinds, "  ")}
	lines = append(lines, "endpoints with no page: "+strconv.Itoa(len(noPage)))
	lines = append(lines, indent("  ", head(noPage, maxRows))...)
	lines = append(lines, "endpoints with no test: "+strconv.Itoa(len(noTest)))
	lines = append(lines, indent("  ", head(noTest, maxRows))...)
	lines = append(lines, "pages with no e2e: "+strconv.Itoa(len(pageNoTest)))
	lines = append(lines, indent("  ", head(pageNoTest, maxRows))...)
	lines = append(lines, CoverageLines(g, false)...)
	if g.Perf != nil {
		lines = append(lines, "performance")
		lines = append(lines, "  last index   "+jsjson.FormatNumber(g.Perf.TotalMs)+"ms · "+PhaseLine(g.Perf, true))
		lines = append(lines, "  graph file   "+graphSize(g)+" · heap "+jsjson.FormatNumber(g.Perf.HeapMb)+"MB")
	}
	return strings.Join(lines, "\n")
}

func graphSize(g *Graph) string {
	n := jsstr.Len(string(g.MarshalJS()))
	if n > 1048576 {
		// toFixed(1): n/2^20 is exact, so ten times it plus a half rounds ties up as JS does.
		tenths := int64(math.Floor(float64(n)/1048576*10 + 0.5))
		return strconv.FormatInt(tenths/10, 10) + "." + strconv.FormatInt(tenths%10, 10) + "MB"
	}
	return jsjson.FormatNumber(jsRound(float64(n)/1024)) + "KB"
}

// CoverageLines is recall, stated plainly: what the extractors found versus what
// the repo contains. full lists every gap instead of the first eight.
func CoverageLines(g *Graph, full bool) []string {
	c := g.Coverage
	if c == nil {
		return []string{"coverage  unknown — re-index with this version of weave"}
	}
	pct := func(a, b int) string {
		if b == 0 {
			return "100"
		}
		return jsjson.FormatNumber(math.Floor(float64(a)/float64(b)*1000) / 10)
	}
	itoa := strconv.Itoa
	out := []string{"coverage"}
	out = append(out, "  route files   "+itoa(c.Routes.Found)+"/"+itoa(c.Routes.Files)+" produced endpoints ("+pct(c.Routes.Found, c.Routes.Files)+"%)")
	calls := "  client calls  " + itoa(c.Calls.Resolved) + "/" + itoa(c.Calls.Total) + " resolvable matched an endpoint (" + pct(c.Calls.Resolved, c.Calls.Total) + "%)"
	if c.Calls.Dynamic != nil && *c.Calls.Dynamic != 0 {
		calls += " · " + itoa(*c.Calls.Dynamic) + " built from a variable, unknowable"
	}
	out = append(out, calls)
	skipped := ""
	if c.Pages.Skipped != 0 {
		skipped = ", " + itoa(c.Pages.Skipped) + " redirect"
	}
	out = append(out, "  routes        "+itoa(c.Pages.Mapped)+"/"+itoa(c.Pages.Declared)+" <Route> entries mapped ("+pct(c.Pages.Mapped, c.Pages.Declared)+"%"+skipped+")")
	out = append(out, "  task files    "+itoa(c.Specs.Parsed)+"/"+itoa(c.Specs.Files)+" parsed ("+pct(c.Specs.Parsed, c.Specs.Files)+"%)")
	out = append(out, "  prisma        "+itoa(c.Prisma.Tables)+"/"+itoa(c.Prisma.Models)+" models mapped to tables")
	if c.Next != nil && (c.Next.Files != 0 || c.Next.Found != 0) {
		out = append(out, "  next routes   "+itoa(c.Next.Found)+"/"+itoa(c.Next.Files)+" route files produced endpoints ("+pct(c.Next.Found, c.Next.Files)+"%)")
	}
	if c.Trpc != nil && (c.Trpc.Files != 0 || c.Trpc.Found != 0) {
		out = append(out, "  tRPC routers  "+itoa(c.Trpc.Found)+"/"+itoa(c.Trpc.Files)+" parsed ("+pct(c.Trpc.Found, c.Trpc.Files)+"%)")
	}
	if c.OpenAPI != nil && c.OpenAPI.Files != 0 {
		out = append(out, "  openapi       "+itoa(c.OpenAPI.Found)+"/"+itoa(c.OpenAPI.Files)+" specs produced endpoints ("+pct(c.OpenAPI.Found, c.OpenAPI.Files)+"%)")
	}
	if c.Patterns != nil && *c.Patterns != 0 {
		out = append(out, "  patterns      "+itoa(*c.Patterns)+" endpoint(s) from .weave/patterns.json")
	}
	silent := append([]string{}, c.Routes.Silent...)
	silent = append(silent, c.Specs.Silent...)
	for _, s := range []*FileStats{c.Next, c.Trpc, c.OpenAPI} {
		if s != nil {
			silent = append(silent, s.Silent...)
		}
	}
	silent = append(silent, c.Calls.Silent...)
	if len(silent) > 0 {
		out = append(out, "  GAPS          "+itoa(len(silent))+" file(s) call an API we did not map (external service, or a pattern we miss):")
		n := 8
		if full {
			n = 40
		}
		out = append(out, indent("    ", head(silent, n))...)
		if !full && len(silent) > 8 {
			out = append(out, "    +"+itoa(len(silent)-8)+" more — weave doctor --all")
		}
		out = append(out, "    an answer that does not mention these may be incomplete")
	}
	return out
}
