package weave

import (
	"regexp"
	"strings"

	"github.com/michalbasisty/weave/internal/jsstr"
)

// ---------------------------------------------------------------- targets

// TargetMap is the files each extractor reads.
type TargetMap struct {
	Backend, Web, Prisma, Specs, All []SourceFile
}

var (
	reTestDir     = jsRe(`(^|\/)(__tests__|e2e|tests?)\/`)
	reTestName    = jsRe(`\.(test|spec)\.[tj]sx?$`)
	reBackendDir  = jsRe(`\/(routes|controller|handler|service)\b`)
	reWebDir      = jsRe(`\/(pages|components|views|hooks)\/`)
	reWebExt      = jsRe(`\.(tsx|jsx|vue|svelte)$`)
	reSpecDir     = jsRe(`(^|\/)specs?\/`)
	reModuleOfDir = jsRe(`modules\/([^/]+)\/`)
)

// Classify splits the walk into backend, web, prisma and spec files.
func Classify(files []SourceFile) TargetMap {
	isTest := func(f *SourceFile) bool { return reTestDir.MatchString(f.Rel) || reTestName.MatchString(f.Rel) }
	var t TargetMap
	for i := range files {
		f := &files[i]
		if !isTest(f) && (strings.HasPrefix(f.Rel, "packages/backend/") || reBackendDir.MatchString(f.Rel)) {
			t.Backend = append(t.Backend, *f)
		}
		if !isTest(f) && (strings.HasPrefix(f.Rel, "packages/web/") || reWebDir.MatchString(f.Rel)) && reWebExt.MatchString(f.Rel) {
			t.Web = append(t.Web, *f)
		}
		if strings.HasSuffix(f.Rel, "schema.prisma") || strings.HasSuffix(f.Rel, ".prisma") {
			t.Prisma = append(t.Prisma, *f)
		}
		if reSpecDir.MatchString(f.Rel) && strings.HasSuffix(f.Rel, ".md") {
			t.Specs = append(t.Specs, *f)
		}
	}
	t.All = files
	return t
}

// ---------------------------------------------------------------- imports

// ResolveImport resolves a relative import spec to a known file, handling `.js`
// suffixes and index files.
func ResolveImport(importer, spec string, known map[string]bool) string {
	if !strings.HasPrefix(spec, ".") {
		return ""
	}
	base := stripCodeExt(JoinPosix(DirOf(importer), spec))
	for _, g := range []string{
		base + ".ts", base + ".tsx", base + ".js", base + ".jsx", base + ".mjs",
		base + "/index.ts", base + "/index.tsx", base + "/index.js",
	} {
		if known[g] {
			return g
		}
	}
	return ""
}

// JoinPosix joins and normalizes without keeping a leading slash or `..` past the root.
func JoinPosix(a, b string) string {
	var out []string
	for _, p := range strings.Split(a+"/"+b, "/") {
		switch p {
		case ".", "":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// ImportEntry is one file's resolved imports, in import order.
type ImportEntry struct {
	From    string
	Targets []string
}

// ImportMap is file -> imported files, in walk order.
type ImportMap struct {
	List []ImportEntry
	by   map[string]int
}

// Get is the targets of rel.
func (m *ImportMap) Get(rel string) []string {
	if i, ok := m.by[rel]; ok {
		return m.List[i].Targets
	}
	return nil
}

// BuildImportMap reads every file's import/export-from specs.
func BuildImportMap(files []SourceFile, known map[string]bool, al *Aliases) *ImportMap {
	m := &ImportMap{by: map[string]int{}}
	for i := range files {
		f := &files[i]
		targets := []string{}
		for _, spec := range importSpecs(f.Text) {
			if r := al.Resolve(f.Rel, spec, known); r != "" && r != f.Rel {
				targets = append(targets, r)
			}
		}
		if j, ok := m.by[f.Rel]; ok {
			m.List[j].Targets = targets
			continue
		}
		m.by[f.Rel] = len(m.List)
		m.List = append(m.List, ImportEntry{f.Rel, targets})
	}
	return m
}

// Reachable is the files reachable from start through imports (BFS, capped).
func Reachable(start string, m *ImportMap, cap int) map[string]bool {
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 && len(seen) < cap {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range m.Get(cur) {
			if seen[next] || len(seen) >= cap {
				continue
			}
			seen[next] = true
			queue = append(queue, next)
		}
	}
	return seen
}

// ---------------------------------------------------------------- endpoints

// Endpoint is one route an extractor found.
type Endpoint struct {
	Method, Path, File string
	Line               int
	Module             string
	ID                 string
}

var (
	// `router.get`, `app.get`, `userRouter.post`, `routes.delete`, `app.all`: the
	// receivers Express code uses. A client (`api.get`) is never one of them.
	reRouterVerb = jsRe("\\b(?:app|server|fastify|router|routes|[A-Za-z_$][\\w$]*Router)\\.(get|post|put|patch|delete|all)\\s*\\(\\s*['\"`](\\/[^'\"`]*)['\"`]")
	reRouterAny  = jsRe(`\b(?:app|server|fastify|router|routes|[A-Za-z_$][\w$]*Router)\.(?:get|post|put|patch|delete|all)\s*\(\s*['"\x60]\/`)
	reMount      = jsRe("app\\.use\\s*\\(\\s*['\"`](\\/api[^'\"`]*)['\"`]\\s*,\\s*(\\w+)\\s*\\)")
	reDefaultImp = jsRe(`import\s+(\w+)\s+from\s+['"](\.{1,2}\/[^'"]+)['"]`)
	// @koa/router takes a path without its slash: Outline's `router.post("users.list", …)`.
	reKoaImport   = jsRe(`from\s+['"](?:@koa\/router|koa-router)['"]|require\(\s*['"](?:@koa\/router|koa-router)['"]\s*\)`)
	reKoaVerb     = jsRe("\\brouter\\.(get|post|put|patch|delete|all)\\s*\\(\\s*['\"`]([A-Za-z_][^'\"`\\s]*)['\"`]")
	reRouterFirst = jsRe("router\\.(?:get|post)\\(\\s*['\"`](\\/[^'\"`]*)['\"`]")
	reTrailSlash  = regexp.MustCompile(`/+$`)
)

// mayDeclareRoutes is a cheap filter before reRouterAny, whose open-ended receiver
// (`*Router`) has no literal prefix for the regexp engine to scan for.
func mayDeclareRoutes(t string) bool {
	return strings.Contains(t, "outer.") || strings.Contains(t, "app.") || strings.Contains(t, "routes.") || strings.Contains(t, "server.") || strings.Contains(t, "fastify.")
}

// ExtractEndpoints finds Express-style routes and the /api mounts that prefix them.
func ExtractEndpoints(files []SourceFile, b *Builder) []Endpoint {
	known := knownSet(files)
	var routeFiles []*SourceFile
	for i := range files {
		// A test drives the routes (`server.post("/api/users.info")`); it declares none.
		if reTestOrSpec.MatchString(files[i].Rel) {
			continue
		}
		if isCode(files[i].Rel) && mayDeclareRoutes(files[i].Text) && (reRouterAny.MatchString(files[i].Text) || reKoaImport.MatchString(files[i].Text)) {
			routeFiles = append(routeFiles, &files[i])
		}
	}
	var endpointList []Endpoint

	// Find the app file: the one mounting the most /api routers.
	var appFile *SourceFile
	best := 0
	for i := range files {
		if !isCode(files[i].Rel) {
			continue
		}
		if n := len(reMount.FindAllStringIndex(files[i].Text, -1)); n > best {
			best = n
			appFile = &files[i]
		}
	}

	type mount struct {
		base string
		line int
	}
	mountOf := map[string]mount{}
	if appFile != nil {
		idents := map[string]string{}
		for _, m := range reDefaultImp.FindAllStringSubmatch(appFile.Text, -1) {
			if r := ResolveImport(appFile.Rel, m[2], known); r != "" {
				idents[m[1]] = r
			}
		}
		for i, line := range appFile.Lines {
			m := reMount.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if target, ok := idents[m[2]]; ok {
				mountOf[target] = mount{reTrailSlash.ReplaceAllString(m[1], ""), i + 1}
			}
		}
		b.Node(fileNode(appFile.Rel))
	}

	// Fall back: no app mounts found -> a single base derived from the router files.
	defaultBase := ""
	if len(mountOf) == 0 {
		var sample []string
		for _, f := range routeFiles {
			if m := reRouterFirst.FindStringSubmatch(f.Text); m != nil && m[1] != "" {
				sample = append(sample, m[1])
			}
		}
		all := len(sample) > 0
		for _, s := range sample {
			all = all && strings.HasPrefix(s, "/api")
		}
		if all {
			defaultBase = "/api/v1"
		}
	}

	for _, f := range routeFiles {
		base := defaultBase
		if m, ok := mountOf[f.Rel]; ok {
			base = m.base
		}
		mod := moduleOf(f.Rel)
		if mod != "" {
			b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(DirOf(f.Rel))})
		}
		b.Node(fileNode(f.Rel))
		matches := reRouterVerb.FindAllStringSubmatchIndex(f.Text, -1)
		if reKoaImport.MatchString(f.Text) {
			matches = append(matches, reKoaVerb.FindAllStringSubmatchIndex(f.Text, -1)...)
		}
		for _, m := range matches {
			line := lineAt(f.Text, m[0])
			method := strings.ToUpper(f.Text[m[2]:m[3]])
			if method == "ALL" {
				method = "ANY"
			}
			sub := f.Text[m[4]:m[5]]
			if sub == "/" {
				sub = ""
			} else if !strings.HasPrefix(sub, "/") {
				sub = "/" + sub
			}
			path := NormalizePath(base + sub)
			eid := EndpointID(method, path)
			desc := CommentAbove(f.Lines, line, 220)
			b.Node(&GNode{ID: eid, Kind: KindEndpoint, Name: method + " " + path, File: Ptr(f.Rel), Line: Ptr(line), Meta: strMeta("module", mod, "desc", desc)})
			b.Edge(eid, FileID(f.Rel), "defined_in")
			if mod != "" {
				b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(DirOf(f.Rel))})
				b.Edge(eid, ModuleID(mod), "in_module")
				b.Edge(ModuleID(mod), FileID(f.Rel), "contains")
			}
			endpointList = append(endpointList, Endpoint{method, path, f.Rel, line, mod, eid})
		}
	}
	return endpointList
}

func knownSet(files []SourceFile) map[string]bool {
	known := make(map[string]bool, len(files))
	for i := range files {
		known[files[i].Rel] = true
	}
	return known
}

func moduleOf(file string) string {
	if m := reModuleOfDir.FindStringSubmatch(file); m != nil {
		return m[1]
	}
	if d := DirOf(file); d != "" {
		return BaseName(d)
	}
	return ""
}

// ---------------------------------------------------------------- prisma

// ModelField is one field of a Prisma model.
type ModelField struct {
	Name, Type string
	Relation   bool
}

// Model is one Prisma model.
type Model struct {
	Name, Table, File string
	Line              int
	Fields            []ModelField
}

var (
	reModelHead  = jsRe(`(?m)^model\s+(\w+)\s*\{`)
	reModelBlock = jsRe(`(?m)^model\s+(\w+)\s*\{([\s\S]*?)^\}`)
	reMapTable   = jsRe(`@@map\(\s*"([^"]+)"`)
	reFieldLine  = jsRe(`^(\w+)\s+(\S+)`)
	reFieldType  = regexp.MustCompile(`[?\[\]]`)
)

// ExtractModels reads Prisma models and tables, then the modules that use them.
func ExtractModels(files []SourceFile, b *Builder, usedIn []SourceFile) []Model {
	var out []Model
	for i := range files {
		f := &files[i]
		modelSet := map[string]bool{}
		for _, m := range reModelHead.FindAllStringSubmatch(f.Text, -1) {
			modelSet[m[1]] = true
		}
		for _, m := range reModelBlock.FindAllStringSubmatchIndex(f.Text, -1) {
			name := f.Text[m[2]:m[3]]
			body := f.Text[m[4]:m[5]]
			table := name
			if t := reMapTable.FindStringSubmatch(body); t != nil {
				table = t[1]
			}
			line := lineAt(f.Text, m[0])
			var fields []ModelField
			for _, raw := range strings.Split(body, "\n") {
				t := jsstr.Trim(raw)
				if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "@@") {
					continue
				}
				fm := reFieldLine.FindStringSubmatch(t)
				if fm == nil {
					continue
				}
				typ := reFieldType.ReplaceAllString(fm[2], "")
				fields = append(fields, ModelField{fm[1], typ, modelSet[typ]})
			}
			mid := ModelID(name)
			desc := CommentAbove(f.Lines, line, 220)
			b.Node(&GNode{ID: mid, Kind: KindModel, Name: name, File: Ptr(f.Rel), Line: Ptr(line), Meta: strMeta("desc", desc)})
			b.Node(&GNode{ID: TableID(table), Kind: KindTable, Name: table, File: Ptr(f.Rel), Meta: strMeta("desc", desc)})
			b.Edge(mid, TableID(table), "maps_to")
			for _, fl := range fields {
				if fl.Relation {
					b.Edge(mid, ModelID(fl.Type), "relates")
				}
			}
			out = append(out, Model{name, table, f.Rel, line, fields})
		}
	}
	// Which models each module touches (prisma.<model> or tx.<model>). Read from the
	// code that uses them, not from the schema file they are declared in.
	modelByLower := map[string]string{}
	for _, m := range out {
		modelByLower[strings.ToLower(m.Name)] = m.Name
	}
	if len(modelByLower) == 0 {
		return out
	}
	for i := range usedIn {
		file := &usedIn[i]
		mod := moduleOf(file.Rel)
		if mod == "" {
			continue
		}
		for _, name := range prismaUses(file.Text) {
			if model, ok := modelByLower[strings.ToLower(name)]; ok {
				b.Use(mod, file.Rel, model)
			}
		}
	}
	return out
}

// reFetchMethod finds the method of a fetch options object.
var reFetchMethod = regexp.MustCompile(`method\s*:\s*['"](\w+)['"]`)

// fetchMethodAhead reads the options object when it spans lines: a service worker
// writes `fetch(url, {\n method: 'POST' })`, which the line-local match read as GET
// and reported as a call to nothing.
func fetchMethodAhead(lines []string, n int, path string) string {
	line := lines[n]
	at := strings.Index(line, path)
	if at < 0 {
		// A path joined with `+` is written in parts: find its first literal.
		lit, _, _ := strings.Cut(path, "${")
		if at = strings.Index(line, lit); at < 0 || lit == "" {
			return ""
		}
		path = lit
	}
	// Start at the options brace, not the first `{`: a template segment `${id}`
	// opens a brace earlier on the same line.
	rest := line[at+len(path):]
	open := strings.IndexByte(rest, '{')
	if open < 0 {
		return ""
	}
	end := n + 12
	if end > len(lines) {
		end = len(lines)
	}
	body := rest[open+1:]
	if n+1 < end {
		body += "\n" + strings.Join(lines[n+1:end], "\n")
	}
	// Only the options object's own keys: `body: JSON.stringify({ … })` closes a
	// brace before `method:` does.
	var own strings.Builder
	depth := 1
	for i := 0; i < len(body) && depth > 0; i++ {
		switch body[i] {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
		default:
			if depth == 1 {
				own.WriteByte(body[i])
			}
		}
	}
	if m := reFetchMethod.FindStringSubmatch(own.String()); m != nil {
		return m[1]
	}
	return ""
}

// ---------------------------------------------------------------- web: pages + calls

// Call is one API call site.
type Call struct {
	Method, Path, File string
	Line               int
	Dynamic            bool
}

// unknowablePath is a call whose path cannot be resolved statically: a variable in
// its first segment (`/${source}`), or a variable glued to literal text
// (`/reports${params}`, `/tasks${qs}`). A whole-segment variable (`/tasks/${id}`) is
// resolvable, so it stays.
func unknowablePath(p string) bool {
	// A query string is not part of the path: `/tasks?status=${s}` is resolvable,
	// `/tasks${qs}` is not.
	p = WithoutQuery(p)
	segs := strings.Split(strings.TrimLeft(jsstr.TrimStart(p), "/"), "/")
	for i, s := range segs {
		j := strings.Index(s, "${")
		if j < 0 {
			continue
		}
		if i == 0 || j > 0 {
			return true
		}
	}
	return false
}

// externalURL reports whether a call names another host: `https://api.github.com/…`.
// Such a call is a contract with someone else's service, never with this repo's
// endpoints, so it can be neither matched nor broken. Loopback hosts are this repo.
func externalURL(p string) bool {
	rest, ok := strings.CutPrefix(p, "https://")
	if !ok {
		rest, ok = strings.CutPrefix(p, "http://")
	}
	if !ok {
		rest, ok = strings.CutPrefix(p, "//")
	}
	if !ok {
		return false
	}
	host := rest
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	switch host {
	case "localhost", "127.0.0.1", "0.0.0.0", "[::1]":
		return false
	}
	return true
}

// ExtractCalls finds axios-style, fetch and tRPC client calls, one per line and kind.
// Calls to another host are left out: see externalURL.
func ExtractCalls(files []SourceFile) []Call {
	var calls []Call
	for i := range files {
		f := &files[i]
		for n, line := range f.Lines {
			if verb, path, ok := callMatch(line); ok && !externalURL(path) {
				calls = append(calls, Call{strings.ToUpper(verb), path, f.Rel, n + 1, strings.Contains(path, "${")})
			}
		}
		for n, line := range f.Lines {
			path, method, ok := fetchMatch(line)
			if !ok || externalURL(path) || (!strings.HasPrefix(path, "/") && !strings.Contains(path, "/api/")) {
				continue
			}
			if method == "" {
				method = fetchMethodAhead(f.Lines, n, path)
			}
			if method == "" {
				method = "GET"
			}
			calls = append(calls, Call{strings.ToUpper(method), path, f.Rel, n + 1, strings.Contains(path, "${")})
		}
		for n, line := range f.Lines {
			proc, hook, ok := trpcMatch(line)
			if !ok {
				continue
			}
			method := "QUERY"
			if hook == "useMutation" || hook == "mutate" {
				method = "MUTATION"
			}
			calls = append(calls, Call{method, "/api/trpc/" + proc, f.Rel, n + 1, false})
		}
	}
	return calls
}

// Page is one routed screen.
type Page struct {
	Route, Component, File string // File is "" when the component did not resolve
}

// PageInfo is extractPages' result.
type PageInfo struct {
	Pages    []Page
	Declared int
	Skipped  []string
}

var (
	reRouteLine = jsRe(`<Route\b(.*?)\bpath=\{?["'\x60]([^"'\x60]+)["'\x60]\}?(.*)$`)
	reNamedImp  = jsRe(`import\s+(?:\w+\s*,\s*)?\{?\s*(\w+)\s*\}?\s*(?:,\s*\w+\s*)?from\s+['"](\.{1,2}\/[^'"]+)['"]`)
	reStarImp   = jsRe(`import\s+\*\s+as\s+(\w+)\s+from\s+['"](\.{1,2}\/[^'"]+)['"]`)
	reLazyImp   = jsRe(`const\s+(\w+)\s*=\s*(?:React\.)?lazy\(\s*\(\)\s*=>\s*import\(\s*(?:\/\*[\s\S]*?\*\/\s*)*['"]([^'"]+)['"]`)
	reLazyChunk = jsRe(`const\s+(\w+)\s*=\s*(?:React\.)?lazy\(\s*(\w+)\.(\w+)\s*\)`)
	reJSXTag    = jsRe(`<\s*([A-Z]\w*)`)
	notAPage    = map[string]bool{"ProtectedRoute": true, "Navigate": true, "Suspense": true, "Fragment": true, "Routes": true, "Route": true}
)

// ExtractPages reads <Route path> declarations and the component each one renders.
func ExtractPages(webFiles []SourceFile, b *Builder, allFiles []SourceFile, al *Aliases) PageInfo {
	// Resolve against every file, not only the JSX subset: the route table often
	// imports a `.ts` chunk map, and a page's own file can be imported from it.
	resolveFiles := allFiles
	if len(resolveFiles) == 0 {
		resolveFiles = webFiles
	}
	known := knownSet(resolveFiles)
	byRel := map[string]*SourceFile{}
	for i := range resolveFiles {
		byRel[resolveFiles[i].Rel] = &resolveFiles[i]
	}
	info := PageInfo{Skipped: []string{}}
	for i := range webFiles {
		f := &webFiles[i]
		if !strings.Contains(f.Text, "<Route") {
			continue
		}
		idents := map[string]string{}
		for _, re := range []*regexp.Regexp{reNamedImp, reStarImp, reLazyImp} {
			for _, m := range re.FindAllStringSubmatch(f.Text, -1) {
				if r := al.Resolve(f.Rel, m[2], known); r != "" {
					idents[m[1]] = r
				}
			}
		}
		// `const Login = lazy(chunks.Login)`: the target lives in the chunk map, one
		// module away. Without this a page node keeps the router file as its own file,
		// and the page-to-endpoint walk starts from the router instead of the screen.
		for _, m := range reLazyChunk.FindAllStringSubmatch(f.Text, -1) {
			mapRel, ok := idents[m[2]]
			if !ok {
				continue
			}
			mapFile := byRel[mapRel]
			if mapFile == nil {
				continue
			}
			key := regexp.QuoteMeta(m[3])
			hit := jsRe(`(?:^|[\s,{])` + key + `\s*:\s*(?:\([^)]*\)\s*=>\s*)?import\(\s*['"]([^'"]+)['"]`).FindStringSubmatch(mapFile.Text)
			if hit == nil {
				continue
			}
			if r := ResolveImport(mapFile.Rel, hit[1], known); r != "" {
				idents[m[1]] = r
			}
		}
		for n, lineText := range f.Lines {
			m := reRouteLine.FindStringSubmatch(lineText)
			if m == nil {
				continue
			}
			line := n + 1
			route := m[2]
			info.Declared++
			component := ""
			// the element may sit before or after path=
			for _, x := range reJSXTag.FindAllStringSubmatch(m[1]+" "+m[3], -1) {
				if !notAPage[x[1]] {
					component = x[1]
					break
				}
			}
			if component == "" {
				info.Skipped = append(info.Skipped, route)
				continue
			}
			pid := PageID(route)
			cf, hasCF := idents[component]
			desc := CommentAbove(f.Lines, line, 220)
			if desc == "" && hasCF {
				var lines []string
				if src := byRel[cf]; src != nil {
					lines = src.Lines
				}
				desc = TopComment(lines, 220)
			}
			file := f.Rel
			if hasCF {
				file = cf
			}
			b.Node(&GNode{ID: pid, Kind: KindPage, Name: route, File: Ptr(file), Line: Ptr(line), Meta: strMeta("component", component, "desc", desc)})
			if hasCF {
				b.Edge(pid, FileID(cf), "renders")
			}
			b.Edge(pid, FileID(f.Rel), "declared_in")
			info.Pages = append(info.Pages, Page{route, component, cf})
		}
	}
	return info
}

var reTemplateExpr = regexp.MustCompile(`\$\{[^}]*\}`)

// alignScore compares an endpoint's segments with a call's from the tail: a
// literal match scores 2, a parameter 0.5. A call may omit the mount prefix, but
// a length mismatch must share at least one literal.
func alignScore(es, cs []string) (float64, bool) {
	n := min(len(es), len(cs))
	if n == 0 {
		return 0, false
	}
	score := 0.0
	literals := 0
	for k := 1; k <= n; k++ {
		a := es[len(es)-k]
		b := cs[len(cs)-k]
		if strings.HasPrefix(a, ":") || a == "*" || a == "**" {
			score += 0.5
			continue
		}
		if b == "*" {
			// A variable leaf names one of many items (`/articles/${slug}`), never a
			// fixed route beside them (`/articles/feed`).
			if k == 1 && len(cs) > 1 {
				return 0, false
			}
			score += 0.4
			continue
		}
		if a == b {
			score += 2
			literals++
			continue
		}
		return 0, false
	}
	// `/orders` is not `/api/reports/:id`, and `/articles/${slug}/comments` is not
	// `/articles/:slug` under a mount prefix, unless a literal lines up.
	if len(cs) != len(es) && literals == 0 {
		return 0, false
	}
	return score, true
}

// MatchEndpoint links a call site to the endpoint it hits. Call sites are written
// relative to their client's base URL and repos mix families (`/api/v1/...`,
// `/api/...`, `/api/trpc/...`), so the comparison aligns on the tail of both paths
// instead of assuming one mount.
func MatchEndpoint(call Call, endpoints []Endpoint) *Endpoint {
	p := call.Path
	p = WithoutQuery(p)
	if strings.Contains(p, "${") {
		// A template literal cut short by an inner quote still names the path before it.
		open := strings.Index(p, "${")
		if strings.IndexByte(p[open:], '}') == -1 {
			p = p[:open]
			if p == "" {
				p = "/"
			}
		} else {
			p = reTemplateExpr.ReplaceAllString(p, "*")
		}
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cs := segments(p)
	if len(cs) == 0 {
		return nil
	}

	var best *Endpoint
	bestScore := -1.0
	for i := range endpoints {
		e := &endpoints[i]
		if e.Method != call.Method && e.Method != "ANY" && call.Method != "ANY" {
			continue
		}
		es := segments(e.Path)
		score, ok := alignScore(es, cs)
		// A catch-all (`[...slug]`, `*path`) takes the rest of the call, one segment
		// or more; an optional one (`[[...slug]]`) takes none as well.
		if last := len(es) - 1; last >= 0 && (es[last] == "*" || es[last] == "**") {
			from := len(cs) - 1
			if es[last] == "**" {
				from = len(cs)
			}
			for l := from; l >= 1; l-- {
				if sc, k := alignScore(es[:last], cs[:l]); k && (!ok || sc > score) {
					score, ok = sc+0.5, true
					break
				}
			}
		}
		if ok && score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func segments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// CommentAbove is the comment block immediately above a 1-based line, as one sentence.
func CommentAbove(lines []string, line, max int) string {
	var out []string
	inBlock := false
	for i, n := line-2, 0; i >= 0 && n < 14; i, n = i-1, n+1 {
		raw := ""
		if i < len(lines) {
			raw = lines[i]
		}
		t := jsstr.Trim(raw)
		if t == "" {
			if len(out) > 0 || inBlock {
				break
			}
			continue
		}
		if inBlock {
			out = append([]string{cleanComment(t)}, out...)
			if strings.HasPrefix(t, "/*") {
				break
			}
			continue
		}
		if strings.HasSuffix(t, "*/") {
			inBlock = true
			out = append([]string{cleanComment(t)}, out...)
			continue
		}
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") {
			out = append([]string{cleanComment(t)}, out...)
			continue
		}
		break
	}
	kept := out[:0:0]
	for _, s := range out {
		if !isNoise(s) {
			kept = append(kept, s)
		}
	}
	return jsstr.TruncateWords(jsstr.Trim(collapseSpace(strings.Join(kept, " "))), max)
}

// TopComment is the first comment block near the top of a file: what the file is for.
func TopComment(lines []string, max int) string {
	for i := 0; i < min(len(lines), 24); i++ {
		t := jsstr.Trim(lines[i])
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") {
			at := len(lines)
			if i+2 <= len(lines) {
				at = nextNonComment(lines, i) + 1
			}
			return CommentAbove(lines, at, max)
		}
	}
	return ""
}

func nextNonComment(lines []string, from int) int {
	i := from
	for i < len(lines) {
		t := jsstr.Trim(lines[i])
		if !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "*") && !strings.HasPrefix(t, "/*") {
			return i
		}
		i++
	}
	return i
}

var (
	reCleanBlock  = regexp.MustCompile(`^/\*\*?`)
	reCleanLine   = regexp.MustCompile(`^//+`)
	reCleanStar   = regexp.MustCompile(`^\*+/?`)
	reCleanEnd    = regexp.MustCompile(`\*/$`)
	reNoiseRule   = jsRe(`^[=\-*~_#\s]+$`)
	reNoiseBanner = jsRe(`^[=\-*~_#]{2,}.*[=\-*~_#]{2,}$`)
)

func cleanComment(t string) string {
	t = reCleanBlock.ReplaceAllString(t, "")
	t = reCleanLine.ReplaceAllString(t, "")
	t = reCleanStar.ReplaceAllString(t, "")
	t = reCleanEnd.ReplaceAllString(t, "")
	return jsstr.Trim(t)
}

// isNoise: rule lines, banner headings and one-word labels are not a description.
func isNoise(t string) bool {
	if reNoiseRule.MatchString(t) || reNoiseBanner.MatchString(t) {
		return true
	}
	n := 0
	for i := 0; i < len(t); i++ {
		if isWord(t[i]) {
			n++
		}
	}
	return n < 12
}

var reMultiSlash = regexp.MustCompile(`/{2,}`)

// NormalizePath collapses repeated slashes and drops a trailing one.
func NormalizePath(p string) string {
	clean := reMultiSlash.ReplaceAllString(p, "/")
	if len(clean) > 1 && strings.HasSuffix(clean, "/") {
		return clean[:len(clean)-1]
	}
	if clean == "" {
		return "/"
	}
	return clean
}

// WithoutQuery cuts a call path at its query string. A `?` inside `${…}` is
// JavaScript (`${team?.id}`, `${a ? b : c}`), not the start of a query.
func WithoutQuery(p string) string {
	depth := 0
	for i := 0; i < len(p); i++ {
		switch {
		case p[i] == '$' && i+1 < len(p) && p[i+1] == '{':
			depth++
			i++
		case p[i] == '{' && depth > 0:
			depth++
		case p[i] == '}' && depth > 0:
			depth--
		case p[i] == '?' && depth == 0:
			return p[:i]
		}
	}
	return p
}
