package weave

import (
	"regexp"
	"sort"
	"strings"
)

// Next.js and tRPC publish their contracts structurally: file paths and router
// objects. No AST needed, but unlike Express nothing is explicit, so every file
// that looks like a contract and yields nothing is reported.

// ExtractStats is what a framework extractor found and what it could not read.
type ExtractStats struct {
	Files     int
	Found     int
	Silent    []string
	Ignored   []string
	Endpoints []Endpoint
}

// ---------------------------------------------------------------- Next.js

var (
	reAppRoute      = jsRe(`(?:^|\/)(?:src\/)?app\/(?:(.*)\/)?route\.(?:t|j)sx?$`)
	reAppPage       = jsRe(`(?:^|\/)(?:src\/)?app\/(?:(.*)\/)?page\.(?:t|j)sx?$`)
	rePagesAPI      = jsRe(`(?:^|\/)(?:src\/)?pages\/api\/(.+)\.(?:t|j)sx?$`)
	rePagesPage     = jsRe(`(?:^|\/)(?:src\/)?pages\/(.+)\.(?:t|j)sx?$`)
	reMethodExport  = jsRe(`export\s+(?:async\s+function|const|let)\s+(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\b`)
	reMethodBranch  = jsRe(`req\.method\s*={2,3}\s*['"]([A-Z]+)['"]`)
	reExportDefault = jsRe(`export\s+default\b`)
	reDynSegment    = jsRe(`^\[(?:\.\.\.)?(.+)\]$`)
	reIndexSuffix   = regexp.MustCompile(`/index$`)
	reBracketParam  = regexp.MustCompile(`\[(?:\.\.\.)?([^\]]+)\]`)
)

// appPath is `app/api/orders/[id]/route.ts` → `/api/orders/:id`.
func appPath(segs string) string {
	var parts []string
	for _, s := range strings.Split(segs, "/") {
		if s == "" || strings.HasPrefix(s, "(") || strings.HasPrefix(s, "@") { // route groups, parallel slots
			continue
		}
		if strings.HasPrefix(s, "[...") || strings.HasPrefix(s, "[[...") {
			parts = append(parts, "*")
		} else {
			parts = append(parts, reDynSegment.ReplaceAllString(s, ":$1"))
		}
	}
	return NormalizePath("/" + strings.Join(parts, "/"))
}

// ExtractNextRoutes reads app-router route handlers and pages/api handlers.
func ExtractNextRoutes(files []SourceFile, b *Builder) ExtractStats {
	st := ExtractStats{Silent: []string{}}
	for i := range files {
		f := &files[i]
		app := reAppRoute.FindStringSubmatch(f.Rel)
		var pages []string
		if app == nil {
			pages = rePagesAPI.FindStringSubmatch(f.Rel)
			if pages == nil {
				continue
			}
		}
		st.Files++

		var path string
		if app != nil {
			path = appPath(app[1])
		} else {
			path = NormalizePath("/api/" + reBracketParam.ReplaceAllString(reIndexSuffix.ReplaceAllString(pages[1], ""), ":$1"))
		}

		var methods []string
		add := func(m string) {
			for _, x := range methods {
				if x == m {
					return
				}
			}
			methods = append(methods, m)
		}
		for _, m := range reMethodExport.FindAllStringSubmatch(f.Text, -1) {
			add(m[1])
		}
		for _, m := range reMethodBranch.FindAllStringSubmatch(f.Text, -1) {
			add(m[1])
		}
		if len(methods) == 0 && reExportDefault.MatchString(f.Text) {
			add("ANY")
		}
		if len(methods) == 0 {
			st.Silent = append(st.Silent, f.Rel)
			continue
		}

		desc := CommentAbove(f.Lines, 1, 220)
		mod := "api"
		if i := strings.Index(f.Rel, "/app/"); i >= 0 {
			mod = f.Rel[i+5:]
			if j := strings.IndexByte(mod, '/'); j >= 0 {
				mod = mod[:j]
			}
		}
		if mod != "" {
			b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(f.Rel)})
		}
		for _, method := range methods {
			eid := EndpointID(method, path)
			b.Node(&GNode{ID: eid, Kind: KindEndpoint, Name: method + " " + path, File: Ptr(f.Rel), Meta: strMeta("module", mod, "desc", desc, "framework", "next")})
			b.Edge(eid, FileID(f.Rel), "defined_in")
			if mod != "" {
				b.Edge(eid, ModuleID(mod), "in_module")
				b.Edge(ModuleID(mod), FileID(f.Rel), "contains")
			}
			st.Endpoints = append(st.Endpoints, Endpoint{method, path, f.Rel, 0, mod, eid})
		}
		st.Found++
	}
	return st
}

// ExtractNextPages reads app-router `page.tsx` and, in a Next project, pages-router
// screens. Next publishes its screens as file paths, so a repo with 14 pages and no
// <Route> would otherwise report zero pages and answer nothing about any of them.
// The pages/ lane needs nextRepo: a Vite or CRA app keeps screens there too.
func ExtractNextPages(files []SourceFile, b *Builder, nextRepo bool) ExtractStats {
	st := ExtractStats{Silent: []string{}}
	for i := range files {
		f := &files[i]
		app := reAppPage.FindStringSubmatch(f.Rel)
		var pages []string
		if app == nil {
			if !nextRepo {
				continue
			}
			pages = rePagesPage.FindStringSubmatch(f.Rel)
			if pages == nil || strings.HasPrefix(pages[1], "api/") {
				continue
			}
			// _app, _document, _error are wrappers, not screens.
			if strings.HasPrefix(pages[1], "_") || strings.Contains(pages[1], "/_") {
				continue
			}
		}
		st.Files++

		var route string
		if app != nil {
			route = appPath(app[1])
		} else {
			segs := reIndexSuffix.ReplaceAllString(pages[1], "")
			if segs == "index" {
				segs = ""
			}
			route = NormalizePath("/" + reBracketParam.ReplaceAllString(segs, ":$1"))
		}

		pid := PageID(route)
		b.Node(&GNode{ID: pid, Kind: KindPage, Name: route, File: Ptr(f.Rel), Meta: strMeta("desc", TopComment(f.Lines, 220), "framework", "next")})
		b.Edge(pid, FileID(f.Rel), "renders")
		st.Found++
	}
	return st
}

// ---------------------------------------------------------------- tRPC

var (
	reRouterBlock   = jsRe(`([A-Za-z_$][\w$]*)\s*:\s*(?:[\w$]+\.)?router\s*\(\s*\{`)
	reRouterAssign  = jsRe(`\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:[\w$.]+\.)?router\s*\(\s*\{`)
	reProcedureVerb = jsRe(`\.(query|mutation|subscription)\s*\(`)
)

// matchBrace is the index of the brace that closes the one at open, or len(text).
func matchBrace(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(text)
}

type routerBlock struct {
	name       string
	start, end int
}

// Procedure is one tRPC procedure with its dotted path.
type Procedure struct {
	Name string
	Kind string // QUERY, MUTATION or SUBSCRIPTION
}

// ScanProcedures finds procedures and names them by the router blocks around them.
// The common shape puts `.input(...)` between the name and the verb, across lines:
//
//	list: publicProcedure
//	  .input(z.object({ limit: z.number() }))
//	  .query(({ input }) => …)
//
// so a property is read as a window from its `key:` to the next top-level comma, and
// the verb is looked up inside that window instead of on one line.
func ScanProcedures(text string) []Procedure {
	var blocks []routerBlock
	for _, m := range reRouterBlock.FindAllStringSubmatchIndex(text, -1) {
		blocks = append(blocks, routerBlock{text[m[2]:m[3]], m[0], matchBrace(text, m[1]-1)})
	}
	// `export const postRouter = router({…})` is the router itself; the alias
	// (`post: postRouter`) elsewhere is what gives it its wire name, so the exported
	// name is only a fallback: strip the suffix tRPC conventions add, and treat the
	// root router as carrying no prefix.
	for _, m := range reRouterAssign.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[2]:m[3]]
		if name == "appRouter" {
			name = ""
		} else {
			name = strings.TrimSuffix(name, "Router")
		}
		blocks = append(blocks, routerBlock{name, m[0], matchBrace(text, m[1]-1)})
	}
	var out []Procedure
	for i, bl := range blocks {
		var children []routerBlock
		for j, c := range blocks {
			if i != j && c.start > bl.start && c.start < bl.end {
				children = append(children, c)
			}
		}
		for _, p := range blockProcedures(text, bl.start, bl.end, children) {
			name := p.name
			if bl.name != "" {
				name = bl.name + "." + name
			}
			out = append(out, Procedure{name, p.kind})
		}
	}
	return out
}

type blockProc struct{ name, kind string }

// blockProcedures reads the top-level `key:` properties of one router body.
func blockProcedures(text string, open, end int, children []routerBlock) []blockProc {
	var out []blockProc
	i := open + 1
	for i < end {
		skipTo := -1
		for _, c := range children {
			if c.start == i || (c.start > i && c.start < i+2) {
				skipTo = c.end
				break
			}
		}
		if skipTo >= 0 {
			i = skipTo + 1
			continue
		}
		if !isIdentChar(text[i]) || (i > 0 && isIdentChar(text[i-1])) {
			i++
			continue
		}
		start := i
		for i < end && isIdentChar(text[i]) {
			i++
		}
		name := text[start:i]
		j := i
		for j < end && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
			j++
		}
		if j >= end || text[j] != ':' {
			continue
		}
		valueEnd := topLevelEnd(text, j+1, end)
		if m := reProcedureVerb.FindStringSubmatch(text[j+1 : valueEnd]); m != nil {
			kind := "SUBSCRIPTION"
			switch m[1] {
			case "query":
				kind = "QUERY"
			case "mutation":
				kind = "MUTATION"
			}
			out = append(out, blockProc{name, kind})
		}
		i = valueEnd + 1
	}
	return out
}

// topLevelEnd is the first comma at depth 0 from i, or end.
func topLevelEnd(text string, i, end int) int {
	depth := 0
	for ; i < end; i++ {
		switch text[i] {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
			if depth < 0 {
				return i
			}
		case ',':
			if depth == 0 {
				return i
			}
		}
	}
	return end
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// ExtractTRPC reads tRPC routers, following aliases and imported procedures across
// files so `team: teamRouter` and `find: findTeamsRoute` produce `team.find`.
func ExtractTRPC(files []SourceFile, b *Builder) ExtractStats {
	st := ExtractStats{Silent: []string{}}
	procs := ResolveTRPC(files)
	byRouter := map[string]bool{}
	for _, p := range procs {
		byRouter[p.router] = true
	}
	mod := "trpc"
	for i := range files {
		f := &files[i]
		// A router file declares a router; a provider/client file does not.
		if !hasRouterCall(f.Text) || !(hasProcedure(f.Text) || byRouter[f.Rel]) {
			continue
		}
		st.Files++
		if !byRouter[f.Rel] {
			st.Silent = append(st.Silent, f.Rel)
			continue
		}
		st.Found++
	}
	if len(procs) == 0 {
		return st
	}
	first := procs[0].router
	b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(first)})
	files2 := map[string]bool{}
	for _, p := range procs {
		path := NormalizePath("/api/trpc/" + p.name)
		eid := EndpointID(p.kind, path)
		b.Node(&GNode{
			ID:   eid,
			Kind: KindEndpoint,
			Name: p.kind + " " + path,
			File: Ptr(p.file),
			Meta: strMeta("module", mod, "framework", "trpc", "desc", strings.ToLower(p.kind)+" "+p.name),
		})
		b.Edge(eid, FileID(p.file), "defined_in")
		b.Edge(eid, ModuleID(mod), "in_module")
		st.Endpoints = append(st.Endpoints, Endpoint{p.kind, path, p.file, p.line, mod, eid})
		files2[p.file] = true
		files2[p.router] = true
	}
	keys := make([]string, 0, len(files2))
	for k := range files2 {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.Edge(ModuleID(mod), FileID(k), "contains")
	}
	return st
}
