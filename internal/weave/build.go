package weave

import (
	"math"
	"regexp"
	"runtime"
	"strings"

	"github.com/michalbasisty/weave/internal/perf"
)

// phasesOf is the timer's laps in the order they ran.
func phasesOf(t *perf.Timer) Phases {
	out := Phases{}
	for _, name := range t.PhaseOrder() {
		out.Set(name, t.Phases()[name])
	}
	return out
}

var (
	reReqMarker  = jsRe(`@req\s+((?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*)`)
	reTestOrSpec = regexp.MustCompile(`\.(test|spec)\.`)
)

// BuildGraph walks root and builds the whole graph, in the TypeScript's
// node and edge insertion order.
func BuildGraph(root string) *Graph {
	tm := perf.NewTimer()
	files := Walk(root)
	tm.Lap("walk")
	known := knownSet(files)
	aliases := LoadAliases(files)
	t := Classify(files)
	b := NewBuilder()

	for i := range files {
		b.Node(fileNode(files[i].Rel))
	}

	// file -> file imports: the only way a change to a shared util can be traced back.
	allImports := BuildImportMap(files, known, aliases)
	for _, e := range allImports.List {
		for _, to := range e.Targets {
			b.Edge(FileID(e.From), FileID(to), "imports")
		}
	}

	endpoints := ExtractEndpoints(files, b)
	nextStats := ExtractNextRoutes(files, b)
	nextPageStats := ExtractNextPages(files, b, isNextRepo(files))
	// Remix / React Router v7 file routes count with Next's: both are file routes.
	rrRoutes, rrPages := ExtractRemixRoutes(files, b)
	nextStats = mergeStats(nextStats, rrRoutes)
	nextPageStats = mergeStats(nextPageStats, rrPages)
	trpcStats := ExtractTRPC(files, b, aliases)
	apiStats := ExtractOpenAPI(files, b)
	nestEndpoints := ExtractNestAndFastify(files, b)
	gqlEndpoints, gqlCalls := ExtractGraphQL(files, b)
	pyGoEndpoints := append(ExtractPythonGo(files, b), ExtractRubyPHP(files, b)...)

	// Patterns from .weave/patterns.json: the escape hatch for a team's own wrapper.
	pat := CompilePatterns(LoadPatterns(root))
	customRoutes := PatternRoutes(files, pat)
	var customEndpoints []Endpoint
	for _, r := range customRoutes {
		path := reMultiSlash.ReplaceAllString(r.Path, "/")
		eid := EndpointID(r.Method, path)
		b.Node(&GNode{ID: eid, Kind: KindEndpoint, Name: r.Method + " " + path, File: Ptr(r.File), Line: Ptr(r.Line), Meta: strMeta("module", "api", "desc", "custom pattern", "framework", "pattern")})
		b.Edge(eid, FileID(r.File), "defined_in")
		customEndpoints = append(customEndpoints, Endpoint{r.Method, path, r.File, r.Line, "api", eid})
	}

	var allEndpoints []Endpoint
	for _, list := range [][]Endpoint{endpoints, nestEndpoints, gqlEndpoints, pyGoEndpoints, nextStats.Endpoints, trpcStats.Endpoints, apiStats.Endpoints, customEndpoints} {
		allEndpoints = append(allEndpoints, list...)
	}
	ExtractModels(t.Prisma, b, files)
	ExtractORMModels(files, b)
	ExtractPythonGoModels(files, b)
	// Any module that declares a <Route> can hold the route table: App.js, router.jsx,
	// src/main.tsx. Reading only the web folders missed every one outside them.
	var routeFiles []SourceFile
	for i := range files {
		if isCode(files[i].Rel) && !reTestOrSpec.MatchString(files[i].Rel) && strings.Contains(files[i].Text, "<Route") {
			routeFiles = append(routeFiles, files[i])
		}
	}
	pageInfo := ExtractPages(routeFiles, b, files, aliases)
	// Screens from <Route> tables and from file routing (Next, Remix, React Router)
	// all reach their calls the same way: through the files they import.
	pages := append(append([]Page{}, pageInfo.Pages...), nextPageStats.Pages...)
	var callFiles []SourceFile
	for i := range files {
		if isCode(files[i].Rel) && !reTestOrSpec.MatchString(files[i].Rel) {
			callFiles = append(callFiles, files[i])
		}
	}
	calls := ExtractCalls(callFiles)
	calls = append(calls, gqlCalls...)
	for _, c := range PatternCalls(files, pat) {
		calls = append(calls, Call{c.Method, c.Path, c.File, c.Line, strings.Contains(c.Path, "${")})
	}
	tm.Lap("extract")

	// MatchEndpoint is pure: resolve each call once for both passes below. A call
	// whose template segment is typed as a literal union is one call per value: each
	// value that matches is linked, each that does not is a broken call.
	matched := make([][]*Endpoint, len(calls))
	unmatched := make([][]string, len(calls))
	unions := unionAliases(files)
	textOf := make(map[string]string, len(files))
	for i := range files {
		textOf[files[i].Rel] = files[i].Text
	}
	for i, c := range calls {
		if paths := expandUnion(c, textOf[c.File], unions); paths != nil {
			for _, p := range paths {
				v := c
				v.Path = p
				if e := MatchEndpoint(v, allEndpoints); e != nil {
					matched[i] = append(matched[i], e)
				} else {
					unmatched[i] = append(unmatched[i], p)
				}
			}
			continue
		}
		if e := MatchEndpoint(c, allEndpoints); e != nil {
			matched[i] = []*Endpoint{e}
		} else {
			matched[i] = variableLeafChoices(c, allEndpoints)
		}
	}

	// page -> endpoint, through the component import graph. Every source file takes
	// part: a screen's data hook often lives in another package, reached by alias.
	imp := allImports
	for _, p := range pages {
		if p.File == "" {
			continue
		}
		scope := Reachable(p.File, imp, 200)
		for i, c := range calls {
			if !scope[c.File] {
				continue
			}
			conf := ConfInferred
			if c.File == p.File && !c.Dynamic {
				conf = ConfExact
			}
			for _, e := range matched[i] {
				b.EdgeConf(PageID(p.Route), e.ID, "calls", conf)
			}
		}
	}

	// Any call site with no page in scope still belongs to its file. A call that
	// matches no endpoint is a broken contract: keep it, never drop it — unless its
	// path starts with a variable, which is unknowable rather than broken.
	dynamicCalls := 0
	lineCache := make(map[string][]string, len(files))
	for i := range files {
		lineCache[files[i].Rel] = files[i].Lines
	}
	// A call whose path starts with a variable is unknowable, not broken: QuickSearch
	// asks `/${source}?search=…` for each source a role may read.
	for i, c := range calls {
		if unknowablePath(c.Path) {
			dynamicCalls++
			continue
		}
		// A fetch of the app root (`/?t=…`) is a page load, not a contract.
		bare := c.Path
		bare = WithoutQuery(bare)
		if strings.TrimLeft(bare, "/") == "" {
			continue
		}
		conf := ConfExact
		if c.Dynamic {
			conf = ConfInferred
		}
		for _, e := range matched[i] {
			b.EdgeConf(FileID(c.File), e.ID, "calls", conf)
		}
		missing := unmatched[i]
		if len(matched[i]) == 0 && missing == nil {
			missing = []string{c.Path}
		}
		if len(missing) == 0 {
			continue
		}
		// A call site may be marked as known-broken debt instead of blocking a merge.
		if lines, ok := lineCache[c.File]; ok && c.Line-1 >= 0 && c.Line-1 < len(lines) && strings.Contains(lines[c.Line-1], "weave-ignore") {
			continue
		}
		for _, p := range missing {
			callID := CallID(c.Method, p)
			b.Node(&GNode{ID: callID, Kind: KindCall, Name: c.Method + " " + p, File: Ptr(c.File), Line: Ptr(c.Line)})
			b.EdgeConf(FileID(c.File), callID, "calls_missing", conf)
		}
	}

	// A requirement named in the code is an exact edge — the one traceability
	// mechanism that survives renames, unlike a file path in a spec. Markers inside a
	// template literal are documentation that quotes the syntax, not an implementation.
	for i := range files {
		f := &files[i]
		if !strings.Contains(f.Text, "@req") {
			continue
		}
		for _, m := range reReqMarker.FindAllStringSubmatchIndex(f.Text, -1) {
			if inTemplate(f.Text, m[0]) {
				continue
			}
			b.Edge(FileID(f.Rel), ReqID(f.Text[m[2]:m[3]]), "implements")
		}
	}

	extractTests(files, allEndpoints, pages, b)
	_, tasks := ExtractSpecs(t.Specs, b)
	tm.Lap("specs")
	// task evidence -> files (memoised: the same paths repeat across hundreds of tasks,
	// and the directory fallback scans every known file)
	evCache := map[string]string{}
	dirs := DirIndex(files)
	for _, task := range tasks {
		tid := TaskID(task.Change + "/" + task.ID)
		for _, ev := range task.Evidence {
			file, ok := evCache[ev]
			if !ok {
				file = ResolveEvidence(ev, known, dirs)
				evCache[ev] = file
			}
			if file == "" {
				continue
			}
			if strings.HasSuffix(file, "/") {
				for k := range files {
					if strings.HasPrefix(files[k].Rel, file) {
						b.EdgeConf(tid, FileID(files[k].Rel), "touches", ConfInferred)
					}
				}
			} else {
				b.Edge(tid, FileID(file), "touches")
			}
		}
	}

	tm.Lap("evidence")
	b.AddUsedModules()
	g := withCoverage(b.Finish(root, len(files)), files, allEndpoints, calls, dynamicCalls, pageInfo, nextStats, nextPageStats, trpcStats, apiStats, len(customRoutes), t.Specs, tasks)
	tm.Lap("coverage")
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	g.Perf = &Perf{TotalMs: tm.Total(), Phases: phasesOf(tm), HeapMb: math.Floor(float64(ms.HeapAlloc)/1048576 + 0.5)}
	return g
}

// inTemplate: a marker inside an unterminated template literal is prose quoting the syntax.
func inTemplate(text string, at int) bool {
	open := false
	for i := 0; i < at; i++ {
		switch text[i] {
		case '\\':
			i++
		case '`':
			open = !open
		}
	}
	return open
}

var (
	reCovTestName = regexp.MustCompile(`\.(test|spec)\.[mc]?[tj]sx?$`)
	reCovTestDir  = regexp.MustCompile(`(^|/)(__tests__|e2e)/`)
	reGenerated   = regexp.MustCompile(`/(generated|__generated__|dist|build)/`)
)

func first12(s []string) []string {
	if len(s) > 12 {
		s = s[:12]
	}
	return append([]string{}, s...)
}

// isNextRepo reports whether a package.json names next: the only signal that
// `pages/x.tsx` is a route rather than a folder of React screens.
func isNextRepo(files []SourceFile) bool {
	for i := range files {
		if strings.HasSuffix(files[i].Rel, "package.json") && strings.Contains(files[i].Text, `"next"`) {
			return true
		}
	}
	return false
}

// withCoverage records what the extractors actually covered. Precision is easy to
// claim; recall is the number that decides whether an answer can be trusted, so it
// is measured and shown.
func withCoverage(g *Graph, files []SourceFile, endpoints []Endpoint, calls []Call, dynamicCalls int, pages PageInfo, nextStats, nextPageStats, trpcStats, apiStats ExtractStats, patternRoutes int, specFiles []SourceFile, tasks []*Task) *Graph {
	isTest := func(rel string) bool { return reCovTestName.MatchString(rel) || reCovTestDir.MatchString(rel) }
	// Same rule as extraction: only code can declare a route. Documentation quotes
	// examples, and a .md file that "produced nothing" is not a coverage gap.
	withEndpoints := map[string]bool{}
	for _, e := range endpoints {
		withEndpoints[e.File] = true
	}
	routeLike := 0
	silentRoutes := []string{}
	for i := range files {
		f := &files[i]
		// indexOf first: running the router regex against every file costs more than the walk.
		if isCode(f.Rel) && !isTest(f.Rel) && strings.Contains(f.Text, "router") && reRouterAny.MatchString(f.Text) {
			routeLike++
			if !withEndpoints[f.Rel] {
				silentRoutes = append(silentRoutes, f.Rel)
			}
		}
	}

	orphan, models, tables := 0, 0, 0
	for _, n := range g.Nodes {
		switch n.Kind {
		case KindCall:
			orphan++
		case KindModel:
			models++
		case KindTable:
			tables++
		}
	}

	// A file that calls an API in a way none of our patterns recognise is the one
	// gap we cannot see from the nodes: measure it from the source instead.
	// Patterns are call-shaped on purpose — a bare word like `got` matches prose.
	callFiles := map[string]bool{}
	for _, c := range calls {
		callFiles[c.File] = true
	}
	silentCalls := []string{}
	for i := range files {
		f := &files[i]
		// A file that defines the client or the procedures is not a call site.
		if hasProcedure(f.Text) || strings.Contains(f.Text, "createTRPC") {
			continue
		}
		if !isTest(f.Rel) && isCode(f.Rel) && !reGenerated.MatchString(f.Rel) && !callFiles[f.Rel] && callish(f.Text) {
			silentCalls = append(silentCalls, f.Rel)
		}
	}

	var taskFiles []*SourceFile
	for i := range specFiles {
		if reTasksFile.MatchString(specFiles[i].Rel) && !reTemplates.MatchString(specFiles[i].Rel) {
			taskFiles = append(taskFiles, &specFiles[i])
		}
	}
	parsed := map[string]bool{}
	for _, t := range tasks {
		parsed[t.Change] = true
	}
	silentSpecs := []string{}
	for _, f := range taskFiles {
		parts := strings.Split(f.Rel, "/")
		if !parsed[parts[len(parts)-2]] {
			silentSpecs = append(silentSpecs, f.Rel)
		}
	}

	var nextPages *FileStats
	if nextPageStats.Files != 0 {
		nextPages = &FileStats{Files: nextPageStats.Files, Found: nextPageStats.Found, Silent: first12(nextPageStats.Silent)}
	}

	total := len(calls) - dynamicCalls
	g.Coverage = &Coverage{
		Routes:   FileStats{Files: routeLike, Found: routeLike - len(silentRoutes), Silent: first12(silentRoutes)},
		Calls:    CallStats{Total: total, Resolved: total - orphan, Dynamic: Ptr(dynamicCalls), Silent: first12(silentCalls)},
		Pages:    PageStats{Declared: pages.Declared, Mapped: pages.Declared - len(pages.Skipped), Skipped: len(pages.Skipped)},
		Specs:    SpecStats{Files: len(taskFiles), Parsed: len(parsed), Silent: silentSpecs},
		Prisma:   PrismaStats{Models: models, Tables: tables},
		Next:     &FileStats{Files: nextStats.Files, Found: nextStats.Found, Silent: first12(nextStats.Silent)},
		NextPage: nextPages,
		Trpc:     &FileStats{Files: trpcStats.Files, Found: trpcStats.Found, Silent: first12(trpcStats.Silent)},
		OpenAPI:  &FileStats{Files: apiStats.Files, Found: apiStats.Found, Silent: first12(apiStats.Silent), Ignored: first12(apiStats.Ignored)},
		Patterns: Ptr(patternRoutes),
	}
	return g
}

var (
	reTestHome   = regexp.MustCompile(`__tests__|tests?$|e2e`)
	reAPILiteral = jsRe("['\"`](\\/api\\/[^'\"`$]+)['\"`]")
	reClientCall = jsRe("\\.(get|post|put|patch|delete)\\s*\\(\\s*[`'\"]([^`'\"\\n]+)[`'\"]")
	reGoto       = jsRe(`(?:page\.goto|goto|visit)\s*\(([^)]*)\)`)
	reGotoPath   = jsRe("['\"`](\\/[^'\"`$]*)['\"`]")
	reUpper      = regexp.MustCompile(`[A-Z]`)
)

func extractTests(files []SourceFile, endpoints []Endpoint, pages []Page, b *Builder) {
	for i := range files {
		f := &files[i]
		if !reTestName.MatchString(f.Rel) { // isTest(rel) && the name rule: the name rule implies isTest
			continue
		}
		tid := TestID(f.Rel)
		b.Node(&GNode{ID: tid, Kind: KindTest, Name: f.Rel, File: Ptr(f.Rel)})

		for _, id := range reqIDs(f.Text, false) {
			b.Edge(tid, ReqID(id), "asserts")
		}
		if strings.Contains(f.Text, "@req") {
			for _, m := range reReqMarker.FindAllStringSubmatch(f.Text, -1) {
				b.Edge(tid, ReqID(m[1]), "verifies")
			}
		}

		if mod := testModule(f.Rel); mod != "" {
			b.Edge(tid, ModuleID(mod), "covers")
		}
		if reTestHome.MatchString(DirOf(f.Rel)) {
			b.Edge(tid, FileID(f.Rel), "in")
		}

		// literal API paths referenced by the test
		for _, m := range reAPILiteral.FindAllStringSubmatch(f.Text, -1) {
			for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
				if e := MatchEndpoint(Call{method, m[1], f.Rel, 0, false}, endpoints); e != nil {
					b.Edge(tid, e.ID, "hits")
				}
			}
		}
		// client calls relative to the API base: admin.get('/machines?limit=1'), tech.get(`/tasks/${id}`)
		for _, m := range reClientCall.FindAllStringSubmatch(f.Text, -1) {
			dynamic := strings.Contains(m[2], "${")
			path := m[2]
			if dynamic {
				path = reTemplateExpr.ReplaceAllString(path, "*")
			}
			if e := MatchEndpoint(Call{strings.ToUpper(m[1]), path, f.Rel, 0, dynamic}, endpoints); e != nil {
				conf := ConfExact
				if dynamic {
					conf = ConfInferred
				}
				b.EdgeConf(tid, e.ID, "hits", conf)
			}
		}
		// e2e navigation: page.goto(BASE + '/tasks')
		for _, line := range f.Lines {
			m := reGoto.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			q := reGotoPath.FindStringSubmatch(m[1])
			if q == nil {
				continue
			}
			if p := matchPage(q[1], pages); p != nil {
				b.Edge(tid, PageID(p.Route), "exercises")
			}
		}
	}
}

func testModule(rel string) string {
	if m := reModuleOfDir.FindStringSubmatch(rel); m != nil {
		return m[1]
	}
	name := reTestName.ReplaceAllString(BaseName(rel), "")
	first := strings.Split(name, ".")[0]
	if first == "" || first == "index" {
		return ""
	}
	if loc := reUpper.FindStringIndex(first); loc != nil {
		first = first[:loc[0]] + strings.ToLower(first[loc[0]:loc[1]]) + first[loc[1]:]
	}
	return first
}

func matchPage(path string, pages []Page) *Page {
	p := path
	p = WithoutQuery(p)
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		p = "/"
	}
	want := segments(p)
	var best *Page
	bestLiteral := -1
	for i := range pages {
		have := segments(pages[i].Route)
		if len(have) != len(want) {
			continue
		}
		literal := 0
		ok := true
		for k := range have {
			if strings.HasPrefix(have[k], ":") {
				continue
			}
			if have[k] != want[k] {
				ok = false
				break
			}
			literal++
		}
		if ok && literal > bestLiteral {
			bestLiteral = literal
			best = &pages[i]
		}
	}
	return best
}

func mergeStats(a, b ExtractStats) ExtractStats {
	a.Files += b.Files
	a.Found += b.Found
	a.Silent = append(a.Silent, b.Silent...)
	a.Ignored = append(a.Ignored, b.Ignored...)
	a.Endpoints = append(a.Endpoints, b.Endpoints...)
	a.Pages = append(a.Pages, b.Pages...)
	return a
}
