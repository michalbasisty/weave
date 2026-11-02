package weave

import (
	"regexp"
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
	Endpoints []Endpoint
}

// ---------------------------------------------------------------- Next.js

var (
	reAppRoute      = jsRe(`(?:^|\/)(?:src\/)?app\/(?:(.*)\/)?route\.(?:t|j)sx?$`)
	rePagesAPI      = jsRe(`(?:^|\/)(?:src\/)?pages\/api\/(.+)\.(?:t|j)sx?$`)
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

// ---------------------------------------------------------------- tRPC

var (
	reRouterBlock = jsRe(`([A-Za-z_$][\w$]*)\s*:\s*(?:[\w$]+\.)?router\s*\(\s*\{`)
	reProcedure   = jsRe(`([A-Za-z_$][\w$]*)\s*:\s*[^,{}\n]*?\.(query|mutation|subscription)\s*\(`)
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

// Procedure is one tRPC procedure with its dotted path.
type Procedure struct {
	Name string
	Kind string // QUERY, MUTATION or SUBSCRIPTION
}

// ScanProcedures finds procedures and names them by the router blocks around them.
func ScanProcedures(text string) []Procedure {
	type block struct {
		name       string
		start, end int
	}
	var blocks []block
	for _, m := range reRouterBlock.FindAllStringSubmatchIndex(text, -1) {
		blocks = append(blocks, block{text[m[2]:m[3]], m[0], matchBrace(text, m[1]-1)})
	}
	var out []Procedure
	for _, m := range reProcedure.FindAllStringSubmatchIndex(text, -1) {
		at := m[0]
		// blocks are in start order already, as the TS sort leaves them
		var parts []string
		for _, bl := range blocks {
			if bl.start < at && at < bl.end {
				parts = append(parts, bl.name)
			}
		}
		parts = append(parts, text[m[2]:m[3]])
		kind := "SUBSCRIPTION"
		switch text[m[4]:m[5]] {
		case "query":
			kind = "QUERY"
		case "mutation":
			kind = "MUTATION"
		}
		out = append(out, Procedure{strings.Join(parts, "."), kind})
	}
	return out
}

// ExtractTRPC reads tRPC router files.
func ExtractTRPC(files []SourceFile, b *Builder) ExtractStats {
	st := ExtractStats{Silent: []string{}}
	for i := range files {
		f := &files[i]
		// A router file declares procedures; a provider/client file does not.
		if !hasProcedure(f.Text) || !hasRouterCall(f.Text) {
			continue
		}
		st.Files++

		procs := ScanProcedures(f.Text)
		if len(procs) == 0 {
			st.Silent = append(st.Silent, f.Rel)
			continue
		}
		mod := "trpc"
		b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(f.Rel)})
		for _, p := range procs {
			path := NormalizePath("/api/trpc/" + p.Name)
			eid := EndpointID(p.Kind, path)
			b.Node(&GNode{
				ID:   eid,
				Kind: KindEndpoint,
				Name: p.Kind + " " + path,
				File: Ptr(f.Rel),
				Meta: strMeta("module", mod, "framework", "trpc", "desc", strings.ToLower(p.Kind)+" "+p.Name),
			})
			b.Edge(eid, FileID(f.Rel), "defined_in")
			b.Edge(eid, ModuleID(mod), "in_module")
			st.Endpoints = append(st.Endpoints, Endpoint{p.Kind, path, f.Rel, 0, mod, eid})
		}
		b.Edge(ModuleID(mod), FileID(f.Rel), "contains")
		st.Found++
	}
	return st
}
