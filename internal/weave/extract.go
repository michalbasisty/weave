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
	base := StripExt(JoinPosix(DirOf(importer), spec))
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
func BuildImportMap(files []SourceFile, known map[string]bool) *ImportMap {
	m := &ImportMap{by: map[string]int{}}
	for i := range files {
		f := &files[i]
		targets := []string{}
		for _, spec := range importSpecs(f.Text) {
			if r := ResolveImport(f.Rel, spec, known); r != "" && r != f.Rel {
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
	reRouterVerb  = jsRe("router\\.(get|post|put|patch|delete)\\s*\\(\\s*['\"`]([^'\"`]*)['\"`]")
	reRouterAny   = jsRe(`router\.(?:get|post|put|patch|delete)\s*\(`)
	reMount       = jsRe("app\\.use\\s*\\(\\s*['\"`](\\/api[^'\"`]*)['\"`]\\s*,\\s*(\\w+)\\s*\\)")
	reDefaultImp  = jsRe(`import\s+(\w+)\s+from\s+['"](\.{1,2}\/[^'"]+)['"]`)
	reRouterFirst = jsRe("router\\.(?:get|post)\\(\\s*['\"`](\\/[^'\"`]*)['\"`]")
	reTrailSlash  = regexp.MustCompile(`/+$`)
)

// ExtractEndpoints finds Express-style routes and the /api mounts that prefix them.
func ExtractEndpoints(files []SourceFile, b *Builder) []Endpoint {
	known := knownSet(files)
	var routeFiles []*SourceFile
	for i := range files {
		if isCode(files[i].Rel) && reRouterAny.MatchString(files[i].Text) {
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
		for _, m := range reRouterVerb.FindAllStringSubmatchIndex(f.Text, -1) {
			line := lineAt(f.Text, m[0])
			method := strings.ToUpper(f.Text[m[2]:m[3]])
			sub := f.Text[m[4]:m[5]]
			if sub == "/" {
				sub = ""
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
				b.Edge(ModuleID(mod), ModelID(model), "uses")
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- web: pages + calls

// Call is one API call site.
type Call struct {
	Method, Path, File string
	Line               int
	Dynamic            bool
}

// ExtractCalls finds axios-style, fetch and tRPC client calls, one per line and kind.
func ExtractCalls(files []SourceFile) []Call {
	var calls []Call
	for i := range files {
		f := &files[i]
		for n, line := range f.Lines {
			if verb, path, ok := callMatch(line); ok {
				calls = append(calls, Call{strings.ToUpper(verb), path, f.Rel, n + 1, strings.Contains(path, "${")})
			}
		}
		for n, line := range f.Lines {
			path, method, ok := fetchMatch(line)
			if !ok || (!strings.HasPrefix(path, "/") && !strings.Contains(path, "/api/")) {
				continue
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
	reRouteLine  = jsRe(`<Route\s+path=["']([^"']+)["'](.*)$`)
	reNamedImp   = jsRe(`import\s+(?:\w+\s*,\s*)?\{?\s*(\w+)\s*\}?\s*(?:,\s*\w+\s*)?from\s+['"](\.{1,2}\/[^'"]+)['"]`)
	reStarImp    = jsRe(`import\s+\*\s+as\s+(\w+)\s+from\s+['"](\.{1,2}\/[^'"]+)['"]`)
	reLazyImp    = jsRe(`const\s+(\w+)\s*=\s*(?:React\.)?lazy\(\s*\(\)\s*=>\s*import\(\s*['"](\.{1,2}\/[^'"]+)['"]`)
	reLazyChunk  = jsRe(`const\s+(\w+)\s*=\s*(?:React\.)?lazy\(\s*(\w+)\.(\w+)\s*\)`)
	reJSXTag     = jsRe(`<\s*([A-Z]\w*)`)
	reRoutesFile = jsRe(`routes?\.tsx?$`)
	notAPage     = map[string]bool{"ProtectedRoute": true, "Navigate": true, "Suspense": true, "Fragment": true, "Routes": true, "Route": true}
)

// ExtractPages reads <Route path> declarations and the component each one renders.
func ExtractPages(webFiles []SourceFile, b *Builder, allFiles []SourceFile) PageInfo {
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
		if !strings.HasSuffix(f.Rel, "App.tsx") && !strings.HasSuffix(f.Rel, "App.jsx") && !reRoutesFile.MatchString(f.Rel) {
			continue
		}
		idents := map[string]string{}
		for _, re := range []*regexp.Regexp{reNamedImp, reStarImp, reLazyImp} {
			for _, m := range re.FindAllStringSubmatch(f.Text, -1) {
				if r := ResolveImport(f.Rel, m[2], known); r != "" {
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
			route := m[1]
			info.Declared++
			component := ""
			for _, x := range reJSXTag.FindAllStringSubmatch(m[2], -1) {
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

// MatchEndpoint links a call site to the endpoint it hits. Call sites are written
// relative to their client's base URL and repos mix families (`/api/v1/...`,
// `/api/...`, `/api/trpc/...`), so the comparison aligns on the tail of both paths
// instead of assuming one mount.
func MatchEndpoint(call Call, endpoints []Endpoint) *Endpoint {
	p := call.Path
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
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
		n := min(len(es), len(cs))
		if n == 0 {
			continue
		}
		score := 0.0
		literals := 0
		ok := true
		for k := 1; k <= n; k++ {
			a := es[len(es)-k]
			b := cs[len(cs)-k]
			if strings.HasPrefix(a, ":") || a == "*" {
				score += 0.5
				continue
			}
			if b == "*" {
				score += 0.4
				continue
			}
			if a == b {
				score += 2
				literals++
				continue
			}
			ok = false
			break
		}
		// A call may omit the mount prefix, but it must share at least one literal
		// segment: otherwise `/orders` would match `/api/reports/:id`.
		if ok && len(cs) < len(es) && literals == 0 {
			ok = false
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
	return jsstr.Slice(jsstr.Trim(collapseSpace(strings.Join(kept, " "))), 0, max)
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
