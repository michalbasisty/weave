package weave

import (
	"fmt"
	"sort"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// Project is src/explore.ts's Project: the payload one repository contributes to
// the explorer page. Field order is the order the TypeScript inserts keys, which
// is the order JSON.stringify writes; AppendJS is the only encoder.
type Project struct {
	Name    string
	Root    string
	BuiltAt string
	Cov     CovSummary
	Counts  CountSummary
	Nodes   []ProjectNode
	Edges   [][4]string
	Flows   []Flow
	Modules []Module
	Changes []Change
	Issues  []Issue
}

// CovSummary is the one number a reader should see before trusting anything else.
type CovSummary struct {
	Gaps     int
	Unmapped int
	Calls    CallCoverage
	Routes   RouteCoverage
	Specs    SpecCoverage
}

// CallCoverage is coverage.calls, reduced to what the page shows.
type CallCoverage struct {
	Resolved int
	Total    int
}

// RouteCoverage is coverage.routes, reduced to found/files.
type RouteCoverage struct {
	Found int
	Files int
}

// SpecCoverage is coverage.specs, reduced to parsed/files.
type SpecCoverage struct {
	Parsed int
	Files  int
}

// CountSummary is what the rail and the summary line count.
type CountSummary struct {
	Pages     int
	Endpoints int
	Modules   int
	Tables    int
	Tests     int
	Changes   int
	Reqs      int
}

// ProjectNode is one node as JSON.stringify writes it: undefined fields are
// omitted, which is why the optional ones are pointers.
type ProjectNode struct {
	I  string
	K  string
	N  string
	F  *string
	L  *int
	X  *string
	D  *string
	T  *string
	DN *int
}

// Flow is a page and the endpoints and tables one hop away.
type Flow struct {
	P string
	E []string
	T []string
}

// Module is a module and its endpoints, all of which the explorer can reach.
type Module struct {
	M string
	E []string
	T []string
}

// Change is a spec change with its task progress and the precomputed blast radius.
type Change struct {
	C     string
	Done  int
	Total int
	Open  []string
	Sum   *string
	R     []ChangeReq
	S     *string
}

// ChangeReq is a requirement a change's tasks satisfy.
type ChangeReq struct {
	ID    string
	Title string
}

// Issue is a group of problems the check gate reports.
type Issue struct {
	Key   string
	Kind  string
	Title string
	Items []string
}

// ExplorePayload is explorePayload(g): the project object without its name.
func ExplorePayload(g *Graph) *Project {
	ix := NewIndex(g)

	// Files appear only where they carry information a person can act on: the
	// evidence a spec task names.
	evidence := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind != "touches" {
			continue
		}
		if n := ix.Node(e.To); n != nil && n.Kind == KindFile && n.File != nil && *n.File != "" {
			evidence[n.ID] = true
		}
	}

	nodes := make([]ProjectNode, 0, len(g.Nodes))
	ids := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		if !explorerKind(n.Kind) && !evidence[n.ID] {
			continue
		}
		pn := ProjectNode{I: n.ID, K: n.Kind, N: n.Name, F: n.File, L: n.Line}
		if n.Kind == KindReq && metaTruthy(n, "suspect") {
			pn.X = Ptr("suspect")
		}
		if v, ok := metaGet(n, "desc"); ok && v.Kind() == jsjson.KindString && v.Str() != "" {
			pn.D = Ptr(v.Str())
		}
		if v, ok := metaGet(n, "title"); ok && v.Kind() == jsjson.KindString && v.Str() != "" {
			pn.T = Ptr(v.Str())
		}
		if n.Kind == KindTask && metaIsTrue(n, "done") {
			pn.DN = Ptr(1)
		}
		nodes = append(nodes, pn)
		ids[n.ID] = true
	}

	// Modules use Prisma models; the explorer speaks tables. Project the hop so a
	// module-to-table link survives into the payload.
	modelTable := map[string]string{}
	for _, e := range g.Edges {
		if e.Kind == "maps_to" {
			modelTable[e.From] = e.To
		}
	}
	tablesOf := func(moduleID string) []string {
		seen := newSet()
		for _, e := range ix.From(moduleID, "uses") {
			if t := modelTable[e.To]; t != "" && ids[t] {
				seen.add(t)
			}
		}
		return seen.items()
	}

	wanted := map[string]bool{
		"calls": true, "hits": true, "exercises": true, "asserts": true, "verifies": true,
		"implements": true, "satisfies": true, "has_task": true, "in_module": true,
		"uses": true, "calls_missing": true, "touches": true, "declared_in": true,
	}
	edges := make([][4]string, 0, len(g.Edges))
	for _, e := range g.Edges {
		if wanted[e.Kind] && ids[e.From] && ids[e.To] {
			edges = append(edges, [4]string{e.From, e.To, e.Kind, e.Conf})
		}
	}

	pages := ix.OfKind(KindPage)
	flows := []Flow{}
	for _, p := range pages {
		eps := newSet()
		for _, e := range ix.From(p.ID, "calls") {
			if ids[e.To] {
				eps.add(e.To)
			}
		}
		if eps.size() == 0 {
			continue
		}
		tables := newSet()
		for _, ep := range eps.items() {
			mod, ok := metaGet(ix.Node(ep), "module")
			if !ok || mod.Kind() != jsjson.KindString {
				continue
			}
			for _, t := range tablesOf(ModuleID(mod.Str())) {
				tables.add(t)
			}
		}
		flows = append(flows, Flow{P: p.ID, E: eps.items(), T: tables.items()})
	}

	modules := []Module{}
	for _, m := range ix.OfKind(KindModule) {
		e := []string{}
		for _, x := range ix.To(m.ID, "in_module") {
			if ids[x.From] {
				e = append(e, x.From)
			}
		}
		if len(e) == 0 {
			continue
		}
		modules = append(modules, Module{M: m.ID, E: e, T: tablesOf(m.ID)})
	}
	for _, m := range modules {
		for _, t := range m.T {
			edges = append(edges, [4]string{m.M, t, "uses", ConfExact})
		}
	}

	changes := []Change{}
	for _, c := range ix.OfKind(KindChange) {
		tasks := []*GNode{}
		for _, e := range ix.From(c.ID, "has_task") {
			if n := ix.Node(e.To); n != nil {
				tasks = append(tasks, n)
			}
		}
		if len(tasks) == 0 {
			continue
		}
		done := 0
		open := []string{}
		reqs := &jsjson.Object{}
		for _, t := range tasks {
			if metaIsTrue(t, "done") {
				done++
			} else if len(open) < 4 {
				open = append(open, metaText(t, "title"))
			}
			for _, e := range ix.From(t.ID, "satisfies") {
				if r := ix.Node(e.To); r != nil {
					reqs.Set(r.Name, jsjson.Str(metaText(r, "title")))
				}
			}
		}
		ch := Change{C: c.ID, Done: done, Total: len(tasks), Open: open, R: []ChangeReq{}}
		if v, ok := metaGet(c, "desc"); ok && v.Kind() == jsjson.KindString {
			ch.Sum = Ptr(v.Str())
		}
		for i := 0; i < reqs.Len(); i++ {
			k, v := reqs.At(i)
			ch.R = append(ch.R, ChangeReq{ID: k, Title: v.Str()})
		}
		changes = append(changes, ch)
	}
	// newest first: b.c.localeCompare(a.c)
	sort.SliceStable(changes, func(i, j int) bool {
		return jsstr.LocaleCompare(changes[j].C, changes[i].C) < 0
	})

	issues := []Issue{}
	broken := []string{}
	for _, c := range ix.OfKind(KindCall) {
		for _, e := range ix.To(c.ID, "") {
			if e.Kind == "calls_missing" {
				broken = append(broken, c.ID)
				break
			}
		}
	}
	if len(broken) > 0 {
		issues = append(issues, Issue{Key: "broken_calls", Kind: "error", Title: "Call sites with no matching endpoint", Items: broken})
	}
	noTest := []string{}
	for _, e := range ix.OfKind(KindEndpoint) {
		if len(ix.To(e.ID, "hits")) == 0 && len(ix.To(e.ID, "covers")) == 0 {
			noTest = append(noTest, e.ID)
		}
	}
	if len(noTest) > 0 {
		issues = append(issues, Issue{Key: "untested_endpoints", Kind: "warn", Title: plural(len(noTest), "endpoint") + " with no direct test", Items: noTest})
	}
	noE2e := []string{}
	for _, p := range pages {
		if len(ix.To(p.ID, "exercises")) == 0 {
			noE2e = append(noE2e, p.ID)
		}
	}
	if len(noE2e) > 0 {
		issues = append(issues, Issue{Key: "pages_without_e2e", Kind: "warn", Title: plural(len(noE2e), "page") + " with no e2e", Items: noE2e})
	}
	suspect := []string{}
	for _, r := range ix.OfKind(KindReq) {
		if metaIsTrue(r, "suspect") {
			suspect = append(suspect, r.ID)
		}
	}
	if len(suspect) > 0 {
		issues = append(issues, Issue{Key: "suspect_reqs", Kind: "warn", Title: "Requirements changed after the code was written", Items: suspect})
	}

	return &Project{
		Root:    g.Root,
		BuiltAt: g.BuiltAt,
		Cov:     coverageOf(g),
		Counts: CountSummary{
			Pages:     len(pages),
			Endpoints: len(ix.OfKind(KindEndpoint)),
			Modules:   len(ix.OfKind(KindModule)),
			Tables:    len(ix.OfKind(KindTable)),
			Tests:     len(ix.OfKind(KindTest)),
			Changes:   len(changes),
			Reqs:      len(ix.OfKind(KindReq)),
		},
		Nodes:   nodes,
		Edges:   edges,
		Flows:   flows,
		Modules: modules,
		Changes: changes,
		Issues:  issues,
	}
}

// plural is `${n} ${word}${n === 1 ? ” : 's'}`.
func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func explorerKind(kind string) bool {
	switch kind {
	case KindPage, KindEndpoint, KindModule, KindTable, KindTest, KindReq, KindChange, KindTask, KindCall:
		return true
	}
	return false
}

// coverageOf is the one number a reader should see before trusting anything else.
func coverageOf(g *Graph) CovSummary {
	c := g.Coverage
	if c == nil {
		return CovSummary{}
	}
	gaps := len(c.Routes.Silent) + len(c.Specs.Silent)
	if c.Next != nil {
		gaps += len(c.Next.Silent)
	}
	if c.Trpc != nil {
		gaps += len(c.Trpc.Silent)
	}
	return CovSummary{
		Gaps:     gaps,
		Unmapped: len(c.Calls.Silent),
		Calls:    CallCoverage{Resolved: c.Calls.Resolved, Total: c.Calls.Total},
		Routes:   RouteCoverage{Found: c.Routes.Found, Files: c.Routes.Files},
		Specs:    SpecCoverage{Parsed: c.Specs.Parsed, Files: c.Specs.Files},
	}
}

// ChangeSummary is the blast radius text for a change record.
func ChangeSummary(g *Graph, slug string) string {
	ix := NewIndex(g)
	c := ix.Node(ChangeID(slug))
	if c == nil {
		return ""
	}
	files := newSet()
	for _, e := range ix.From(c.ID, "has_task") {
		for _, t := range ix.From(e.To, "touches") {
			if n := ix.Node(t.To); n != nil && n.File != nil && *n.File != "" {
				files.add(*n.File)
			}
		}
	}
	if files.size() == 0 {
		return ""
	}
	f := Analyze(g, files.items())
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%d endpoint(s), %d page(s), %d table(s), %d test(s)",
		len(f.Endpoints), len(f.Pages), len(f.Tables), len(f.Tests))
}

// AppendJS appends JSON.stringify(project) to b, in the TypeScript's key order.
func (p *Project) AppendJS(b []byte) []byte { return jsjson.Append(b, p.value()) }

func (p *Project) value() jsjson.Value {
	o := &jsjson.Object{}
	o.Set("name", jsjson.Str(p.Name))
	o.Set("root", jsjson.Str(p.Root))
	o.Set("builtAt", jsjson.Str(p.BuiltAt))

	cov := &jsjson.Object{}
	cov.Set("gaps", jsjson.Int(p.Cov.Gaps))
	cov.Set("unmapped", jsjson.Int(p.Cov.Unmapped))
	calls := &jsjson.Object{}
	calls.Set("resolved", jsjson.Int(p.Cov.Calls.Resolved))
	calls.Set("total", jsjson.Int(p.Cov.Calls.Total))
	cov.Set("calls", jsjson.Obj(calls))
	routes := &jsjson.Object{}
	routes.Set("found", jsjson.Int(p.Cov.Routes.Found))
	routes.Set("files", jsjson.Int(p.Cov.Routes.Files))
	cov.Set("routes", jsjson.Obj(routes))
	specs := &jsjson.Object{}
	specs.Set("parsed", jsjson.Int(p.Cov.Specs.Parsed))
	specs.Set("files", jsjson.Int(p.Cov.Specs.Files))
	cov.Set("specs", jsjson.Obj(specs))
	o.Set("cov", jsjson.Obj(cov))

	counts := &jsjson.Object{}
	counts.Set("pages", jsjson.Int(p.Counts.Pages))
	counts.Set("endpoints", jsjson.Int(p.Counts.Endpoints))
	counts.Set("modules", jsjson.Int(p.Counts.Modules))
	counts.Set("tables", jsjson.Int(p.Counts.Tables))
	counts.Set("tests", jsjson.Int(p.Counts.Tests))
	counts.Set("changes", jsjson.Int(p.Counts.Changes))
	counts.Set("reqs", jsjson.Int(p.Counts.Reqs))
	o.Set("counts", jsjson.Obj(counts))

	nodes := make([]jsjson.Value, len(p.Nodes))
	for i := range p.Nodes {
		nodes[i] = p.Nodes[i].value()
	}
	o.Set("nodes", jsjson.Arr(nodes...))

	edges := make([]jsjson.Value, len(p.Edges))
	for i, e := range p.Edges {
		edges[i] = jsjson.Arr(jsjson.Str(e[0]), jsjson.Str(e[1]), jsjson.Str(e[2]), jsjson.Str(e[3]))
	}
	o.Set("edges", jsjson.Arr(edges...))

	flows := make([]jsjson.Value, len(p.Flows))
	for i, f := range p.Flows {
		x := &jsjson.Object{}
		x.Set("p", jsjson.Str(f.P))
		x.Set("e", strArray(f.E))
		x.Set("t", strArray(f.T))
		flows[i] = jsjson.Obj(x)
	}
	o.Set("flows", jsjson.Arr(flows...))

	modules := make([]jsjson.Value, len(p.Modules))
	for i, m := range p.Modules {
		x := &jsjson.Object{}
		x.Set("m", jsjson.Str(m.M))
		x.Set("e", strArray(m.E))
		x.Set("t", strArray(m.T))
		modules[i] = jsjson.Obj(x)
	}
	o.Set("modules", jsjson.Arr(modules...))

	changes := make([]jsjson.Value, len(p.Changes))
	for i, c := range p.Changes {
		x := &jsjson.Object{}
		x.Set("c", jsjson.Str(c.C))
		x.Set("done", jsjson.Int(c.Done))
		x.Set("total", jsjson.Int(c.Total))
		x.Set("open", strArray(c.Open))
		// The TypeScript object literal inserts sum before r; s is assigned after.
		if c.Sum != nil {
			x.Set("sum", jsjson.Str(*c.Sum))
		}
		reqs := make([]jsjson.Value, len(c.R))
		for j, r := range c.R {
			y := &jsjson.Object{}
			y.Set("id", jsjson.Str(r.ID))
			y.Set("title", jsjson.Str(r.Title))
			reqs[j] = jsjson.Obj(y)
		}
		x.Set("r", jsjson.Arr(reqs...))
		if c.S != nil {
			x.Set("s", jsjson.Str(*c.S))
		}
		changes[i] = jsjson.Obj(x)
	}
	o.Set("changes", jsjson.Arr(changes...))

	issues := make([]jsjson.Value, len(p.Issues))
	for i, g := range p.Issues {
		x := &jsjson.Object{}
		x.Set("key", jsjson.Str(g.Key))
		x.Set("kind", jsjson.Str(g.Kind))
		x.Set("title", jsjson.Str(g.Title))
		x.Set("items", strArray(g.Items))
		issues[i] = jsjson.Obj(x)
	}
	o.Set("issues", jsjson.Arr(issues...))

	return jsjson.Obj(o)
}

func (n *ProjectNode) value() jsjson.Value {
	o := &jsjson.Object{}
	o.Set("i", jsjson.Str(n.I))
	o.Set("k", jsjson.Str(n.K))
	o.Set("n", jsjson.Str(n.N))
	if n.F != nil {
		o.Set("f", jsjson.Str(*n.F))
	}
	if n.L != nil {
		o.Set("l", jsjson.Int(*n.L))
	}
	if n.X != nil {
		o.Set("x", jsjson.Str(*n.X))
	}
	if n.D != nil {
		o.Set("d", jsjson.Str(*n.D))
	}
	if n.T != nil {
		o.Set("t", jsjson.Str(*n.T))
	}
	if n.DN != nil {
		o.Set("dn", jsjson.Int(*n.DN))
	}
	return jsjson.Obj(o)
}

func strArray(a []string) jsjson.Value {
	vs := make([]jsjson.Value, len(a))
	for i, s := range a {
		vs[i] = jsjson.Str(s)
	}
	return jsjson.Arr(vs...)
}
