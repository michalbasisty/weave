package weave

import (
	"regexp"
	"sort"
	"strings"
)

// Cross-file tRPC resolution. Real routers are split across files:
//
//	// router.ts
//	export const appRouter = router({ team: teamRouter })
//	// team-router/router.ts
//	export const teamRouter = router({ find: findTeamsRoute, member: { get: getMemberRoute } })
//	// team-router/find-teams.ts
//	export const findTeamsRoute = authenticatedProcedure.input(…).query(…)
//
// so a procedure's wire name (`team.find`) is built by walking from the root router
// through aliases, nested object literals and imported procedure values. Reading one
// file at a time names none of these, and every client call to them looks broken.

var (
	reRouterDef  = jsRe(`\b(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::\s*[^=]+)?=\s*(?:[\w$.]+\.)?(?:router|createTRPCRouter|createRouter|mergeRouters)\s*\(\s*\{`)
	reInlineRtr  = regexp.MustCompile(`^(?:[\w$.]+\.)?(?:router|createTRPCRouter|createRouter)\s*\(\s*\{`)
	reIdentValue = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)
	reConstDef   = jsRe(`\b(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::\s*[^=]+)?=`)
	reImportList = jsRe(`import\s+(?:type\s+)?(?:[\w$]+\s*,\s*)?\{([^}]*)\}\s*from\s*['"]([^'"]+)['"]`)
)

type trpcRouterDef struct {
	name string
	file *SourceFile
	open int // index of the router body's `{`
	end  int
}

type trpcProc struct {
	name, kind, file string
	line             int
	router           string // the router file that names it
}

type trpcResolver struct {
	files   map[string]*SourceFile
	known   map[string]bool
	routers map[string][]trpcRouterDef   // by variable name
	defs    map[string][]string          // const name → files that declare it
	defAt   map[string]int               // "file#name" → index just past the `=`
	imports map[string]map[string]string // file → local name → resolved file
	seen    map[string]bool              // router bodies on the current path (cycles)
	out     []trpcProc
	used    map[string]bool // routers reached from another router
}

func newTRPCResolver(files []SourceFile) *trpcResolver {
	r := &trpcResolver{
		files:   map[string]*SourceFile{},
		known:   knownSet(files),
		routers: map[string][]trpcRouterDef{},
		defs:    map[string][]string{},
		defAt:   map[string]int{},
		imports: map[string]map[string]string{},
		seen:    map[string]bool{},
		used:    map[string]bool{},
	}
	for i := range files {
		f := &files[i]
		if !isCode(f.Rel) && !strings.HasSuffix(f.Rel, ".mts") && !strings.HasSuffix(f.Rel, ".mjs") {
			continue
		}
		r.files[f.Rel] = f
		// Only files that declare a procedure or a router can hold either.
		if !strings.Contains(f.Text, "rocedure") && !strings.Contains(f.Text, "outer(") {
			continue
		}
		for _, m := range reConstDef.FindAllStringSubmatchIndex(f.Text, -1) {
			n := f.Text[m[2]:m[3]]
			if _, dup := r.defAt[f.Rel+"#"+n]; !dup {
				r.defs[n] = append(r.defs[n], f.Rel)
				r.defAt[f.Rel+"#"+n] = m[1]
			}
		}
		for _, m := range reRouterDef.FindAllStringSubmatchIndex(f.Text, -1) {
			if inComment(f.Text, m[0]) {
				continue
			}
			open := m[1] - 1
			r.routers[f.Text[m[2]:m[3]]] = append(r.routers[f.Text[m[2]:m[3]]], trpcRouterDef{f.Text[m[2]:m[3]], f, open, matchBrace(f.Text, open)})
		}
	}
	return r
}

// inComment reports whether i sits on a line comment.
func inComment(text string, i int) bool {
	ls := strings.LastIndexByte(text[:i], '\n') + 1
	return strings.Contains(text[ls:i], "//")
}

// importSource is the file an identifier is imported from in f, or "".
func (r *trpcResolver) importSource(f *SourceFile, ident string) string {
	imp, ok := r.imports[f.Rel]
	if !ok {
		imp = map[string]string{}
		for _, m := range reImportList.FindAllStringSubmatch(f.Text, -1) {
			src := ResolveImport(f.Rel, m[2], r.known)
			if src == "" {
				continue
			}
			for _, part := range strings.Split(m[1], ",") {
				part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "type "))
				local := part
				if k := strings.Index(part, " as "); k >= 0 {
					local = strings.TrimSpace(part[k+4:])
				}
				if local != "" {
					imp[local] = src
				}
			}
		}
		r.imports[f.Rel] = imp
	}
	return imp[ident]
}

// lookup finds where ident is declared as seen from f: the same file, the file it
// is imported from, or the only file in the repo that declares it.
func (r *trpcResolver) lookup(f *SourceFile, ident string, in []string) string {
	has := func(rel string) bool {
		for _, x := range in {
			if x == rel {
				return true
			}
		}
		return false
	}
	if has(f.Rel) {
		return f.Rel
	}
	if src := r.importSource(f, ident); src != "" && has(src) {
		return src
	}
	// Re-exported through an index file, or imported by a package alias: trust a
	// name only when it is declared once.
	if len(in) == 1 {
		return in[0]
	}
	return ""
}

func (r *trpcResolver) routerFor(f *SourceFile, ident string) *trpcRouterDef {
	defs := r.routers[ident]
	if len(defs) == 0 {
		return nil
	}
	rels := make([]string, len(defs))
	for i, d := range defs {
		rels[i] = d.file.Rel
	}
	rel := r.lookup(f, ident, rels)
	for i := range defs {
		if defs[i].file.Rel == rel {
			return &defs[i]
		}
	}
	return nil
}

func procKind(window string) (string, int) {
	m := reProcedureVerb.FindStringSubmatchIndex(window)
	if m == nil {
		return "", -1
	}
	switch window[m[2]:m[3]] {
	case "query":
		return "QUERY", m[0]
	case "mutation":
		return "MUTATION", m[0]
	}
	return "SUBSCRIPTION", m[0]
}

// walkRouter reads one router body under prefix.
func (r *trpcResolver) walkRouter(d *trpcRouterDef, prefix string, depth int) {
	key := d.file.Rel + "#" + itoa(d.open)
	if r.seen[key] || depth > 12 {
		return
	}
	r.seen[key] = true
	defer delete(r.seen, key)
	r.walkObject(d.file, d.open, d.end, prefix, depth)
}

// walkObject reads the top-level `key: value` properties of an object literal.
func (r *trpcResolver) walkObject(f *SourceFile, open, end int, prefix string, depth int) {
	text := f.Text
	i := open + 1
	for i < end {
		c := text[i]
		// skip comments and strings so a `key:` inside them is not read
		if c == '/' && i+1 < end && text[i+1] == '/' {
			for i < end && text[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < end && text[i+1] == '*' {
			k := strings.Index(text[i+2:end], "*/")
			if k < 0 {
				return
			}
			i += k + 4
			continue
		}
		if !isIdentChar(c) || (i > 0 && isIdentChar(text[i-1])) {
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
			// shorthand `{ teamRouter }` or a spread is not a named property
			continue
		}
		valueEnd := topLevelEnd(text, j+1, end)
		value := strings.TrimSpace(text[j+1 : valueEnd])
		full := name
		if prefix != "" {
			full = prefix + "." + name
		}
		vs := j + 1 + strings.Index(text[j+1:valueEnd], value)
		switch {
		case strings.HasPrefix(value, "{"):
			r.walkObject(f, vs, matchBrace(text, vs), full, depth+1)
		case reInlineRtr.MatchString(value):
			ob := vs + strings.IndexByte(value, '{')
			r.walkObject(f, ob, matchBrace(text, ob), full, depth+1)
		case reIdentValue.MatchString(value):
			if d := r.routerFor(f, value); d != nil {
				r.used[d.file.Rel+"#"+d.name] = true
				r.walkRouter(d, full, depth+1)
				break
			}
			if kind, file, line := r.procValue(f, value); kind != "" {
				r.out = append(r.out, trpcProc{full, kind, file, line, f.Rel})
			}
		default:
			if kind, at := procKind(value); kind != "" {
				r.out = append(r.out, trpcProc{full, kind, f.Rel, lineAt(text, vs+at), f.Rel})
			}
		}
		i = valueEnd + 1
	}
}

// procValue resolves a procedure held in a variable: `find: findTeamsRoute`.
func (r *trpcResolver) procValue(f *SourceFile, ident string) (kind, file string, line int) {
	rel := r.lookup(f, ident, r.defs[ident])
	if rel == "" {
		return "", "", 0
	}
	df := r.files[rel]
	if df == nil {
		return "", "", 0
	}
	from := r.defAt[rel+"#"+ident]
	// the declaration runs to the next declaration
	end := len(df.Text)
	if n := reConstDef.FindStringIndex(df.Text[from:]); n != nil {
		end = from + n[0]
	}
	k, at := procKind(df.Text[from:end])
	if k == "" {
		return "", "", 0
	}
	return k, rel, lineAt(df.Text, from+at)
}

// ResolveTRPC names every procedure reachable from a root router, across files.
// A root is a router no other router references; `appRouter` carries no prefix,
// other roots keep their variable name without the `Router` suffix.
func ResolveTRPC(files []SourceFile) []trpcProc {
	r := newTRPCResolver(files)
	names := make([]string, 0, len(r.routers))
	for n := range r.routers {
		names = append(names, n)
	}
	sort.Strings(names)
	// first pass: find which routers are referenced
	for _, n := range names {
		for i := range r.routers[n] {
			r.walkRouter(&r.routers[n][i], "", 0)
		}
	}
	used := r.used
	r.out = nil
	r.used = map[string]bool{}
	var roots []*trpcRouterDef
	for _, n := range names {
		for i := range r.routers[n] {
			d := &r.routers[n][i]
			if !used[d.file.Rel+"#"+d.name] {
				roots = append(roots, d)
			}
		}
	}
	for _, d := range roots {
		prefix := ""
		if !strings.EqualFold(d.name, "appRouter") {
			prefix = strings.TrimSuffix(d.name, "Router")
		}
		r.walkRouter(d, prefix, 0)
	}
	// one name per kind
	seen := map[string]bool{}
	out := r.out[:0]
	for _, p := range r.out {
		k := p.kind + " " + p.name
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}
