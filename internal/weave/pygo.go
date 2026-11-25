package weave

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Python and Go back ends, so a React front end that talks to FastAPI or gin is
// checked like one that talks to Express:
//
//	@router.get("/items/{item_id}")              → GET /items/:item_id      (FastAPI)
//	@bp.route("/users/<int:id>", methods=[…])    → GET|POST /users/:id      (Flask)
//	path("articles/<slug:slug>/", views.detail)  → ANY /articles/:slug/     (Django)
//	v1 := r.Group("/api/v1"); v1.GET("/users")   → GET /api/v1/users        (gin, echo)
//	r.Route("/api", func(r chi.Router) {…})      → prefix for the routes inside (chi)
//	mux.HandleFunc("GET /orders/{id}", h)        → GET /orders/:id          (net/http)
//
// Prefixes are followed where they are written: an APIRouter(prefix=…) or
// Blueprint(url_prefix=…) in the same file, include_router / register_blueprint
// with a prefix naming the router's module, Django include() chains, and gin,
// echo, chi and gorilla groups in the same Go file.

// pyCode is the text with docstrings and comment lines blanked, offsets kept: a
// urls.py header that shows `url(r'^$', views.home)` as an example is not a route.
func pyCode(t string) string {
	b := []byte(t)
	blank := func(i, j int) {
		for k := i; k < j && k < len(b); k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
	}
	for _, q := range []string{`"""`, `'''`} {
		for i := 0; ; {
			s := strings.Index(string(b[i:]), q)
			if s < 0 {
				break
			}
			s += i
			e := strings.Index(string(b[s+3:]), q)
			if e < 0 {
				blank(s, len(b))
				break
			}
			e += s + 3 + 3
			blank(s, e)
			i = e
		}
	}
	lines := strings.SplitAfter(string(b), "\n")
	off := 0
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimLeft(ln, " \t"), "#") {
			blank(off, off+len(ln))
		}
		off += len(ln)
	}
	return string(b)
}

func isPython(rel string) bool { return strings.HasSuffix(rel, ".py") }
func isGo(rel string) bool     { return strings.HasSuffix(rel, ".go") }

func isPyOrGoTest(rel string) bool {
	base := path.Base(rel)
	if strings.HasPrefix(rel, "tests/") || strings.Contains(rel, "/tests/") {
		return true
	}
	return strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || base == "conftest.py"
}

var (
	rePyDecorator  = regexp.MustCompile(`(?m)^[ \t]*@([A-Za-z_]\w*)\.(get|post|put|patch|delete|options|head|route|api_route)\(\s*(?:(?:path|rule)\s*=\s*)?[rRuUbB]?["']([^"']*)["']([^)]*)\)`)
	rePyRouterVar  = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_]\w*)\s*=\s*(?:\w+\.)?(APIRouter|Blueprint)\(([^)]*)\)`)
	rePyPrefixKw   = regexp.MustCompile(`(?:url_)?prefix\s*=\s*[rRuU]?["']([^"']*)["']`)
	rePyMethodsKw  = regexp.MustCompile(`methods\s*=\s*[\[(]([^\])]*)`)
	rePyInclude    = regexp.MustCompile(`\.(?:include_router|register_blueprint)\(\s*([A-Za-z_][\w.]*)\s*(?:,([^)]*))?\)`)
	rePyFromImport = regexp.MustCompile(`(?m)^[ \t]*from\s+([\w.]+)\s+import\s+([^\n#]+)`)
	rePyParam      = regexp.MustCompile(`\{([A-Za-z_]\w*)(?::[^}]*)?\}|<(?:[A-Za-z_]\w*:)?([A-Za-z_]\w*)>`)
	rePyQuoted     = regexp.MustCompile(`["']([A-Za-z]+)["']`)

	reDjangoPath    = regexp.MustCompile(`\b(path|re_path|url)\(\s*[rRuU]?["']([^"']*)["']\s*,\s*(include\(\s*(?:[rRuU]?["']([\w.]+)["']|([A-Za-z_]\w*)\.urls)[^)]*\)|[^,)\n]+)`)
	reDjangoRegster = regexp.MustCompile(`\b([A-Za-z_]\w*)\.register\(\s*[rRuU]?["']([^"']*)["']`)
	reDjangoNamedX  = regexp.MustCompile(`/\?$`)
	reDjangoNamed   = regexp.MustCompile(`\(\?P<([A-Za-z_]\w*)>[^)]*\)`)

	reGoGroup    = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*:?=\s*([A-Za-z_]\w*)\.(?:Group|PathPrefix)\(\s*"([^"]*)"`)
	reGoChiRoute = regexp.MustCompile(`\b([A-Za-z_]\w*)\.Route\(\s*"([^"]*)"\s*,\s*func\(\s*([A-Za-z_]\w*)\s+[\w.*]+\)\s*\{`)
	reGoRoute    = regexp.MustCompile(`\b([A-Za-z_]\w*)\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|Any|Get|Post|Put|Patch|Delete|Head|Options|Handle|HandleFunc|Method|MethodFunc)\(\s*(?:"([A-Z]+)"\s*,\s*)?"([^"]*)"`)
	reGoMethods  = regexp.MustCompile(`^[^\n]*?\.Methods\(([^)]*)\)`)
	reGoParam    = regexp.MustCompile(`\{([A-Za-z_]\w*)(?::[^}]*)?\}`)
)

// pyGoPath writes a route the way the rest of the graph does: a leading slash,
// `:name` params, no doubled slashes. A trailing slash is kept, as Django means it.
func pyGoPath(prefix, p string) string {
	full := strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(p, "/")
	if p == "" || p == "/" {
		full = prefix
		if full == "" {
			full = "/"
		}
	}
	full = rePyParam.ReplaceAllStringFunc(full, func(m string) string {
		sm := rePyParam.FindStringSubmatch(m)
		if sm[1] != "" {
			return ":" + sm[1]
		}
		return ":" + sm[2]
	})
	full = reGoParam.ReplaceAllString(full, ":$1")
	full = reMultiSlash.ReplaceAllString(full, "/")
	if !strings.HasPrefix(full, "/") {
		full = "/" + full
	}
	return full
}

type pyGoAdder struct {
	b    *Builder
	out  []Endpoint
	seen map[string]bool
	mods map[string]bool
}

func (a *pyGoAdder) add(f *SourceFile, method, p string, at int, mod, framework string) {
	eid := EndpointID(method, p)
	if a.seen[eid] {
		return
	}
	a.seen[eid] = true
	line := lineAt(f.Text, at)
	if !a.mods[mod] {
		a.mods[mod] = true
		a.b.Node(&GNode{ID: ModuleID(mod), Kind: KindModule, Name: mod, File: Ptr(DirOf(f.Rel))})
	}
	a.b.Node(&GNode{ID: eid, Kind: KindEndpoint, Name: method + " " + p, File: Ptr(f.Rel), Line: Ptr(line), Meta: strMeta("module", mod, "desc", CommentAbove(f.Lines, line, 220), "framework", framework)})
	a.b.Edge(eid, FileID(f.Rel), "defined_in")
	a.b.Edge(eid, ModuleID(mod), "in_module")
	a.b.Edge(ModuleID(mod), FileID(f.Rel), "contains")
	a.out = append(a.out, Endpoint{method, p, f.Rel, line, mod, eid})
}

func stem(rel string) string { return strings.TrimSuffix(path.Base(rel), path.Ext(rel)) }

// pyModule names a Python file's module: its own name, or the package's for
// routes.py / views.py / urls.py / __init__.py, which say nothing on their own.
func pyModule(rel string) string {
	s := stem(rel)
	switch s {
	case "__init__", "routes", "views", "urls", "api", "endpoints", "main", "app",
		// the files a Django or Flask app splits itself into belong to the app
		"models", "serializers", "relations", "signals", "backends", "admin", "forms", "managers", "renderers":
		if d := path.Base(path.Dir(rel)); d != "." && d != "/" {
			return d
		}
		return "api"
	}
	return s
}

// ExtractPythonGo reads FastAPI, Flask, Django, net/http, gin, echo, chi and
// gorilla routes.
func ExtractPythonGo(files []SourceFile, b *Builder) []Endpoint {
	a := &pyGoAdder{b: b, seen: map[string]bool{}, mods: map[string]bool{}}
	var py, gofiles []*SourceFile
	for i := range files {
		f := &files[i]
		if isPyOrGoTest(f.Rel) {
			continue
		}
		switch {
		case isPython(f.Rel):
			cp := *f
			cp.Text = pyCode(f.Text)
			py = append(py, &cp)
		case isGo(f.Rel):
			gofiles = append(gofiles, f)
		}
	}
	extractPython(py, a)
	extractDjango(py, a)
	extractGo(gofiles, a)
	return a.out
}

func extractPython(files []*SourceFile, a *pyGoAdder) {
	// include_router(users.router, prefix="/api") mounts the router of module users
	mounts := map[string]string{}
	for _, f := range files {
		if !strings.Contains(f.Text, "include_router") && !strings.Contains(f.Text, "register_blueprint") {
			continue
		}
		imported := map[string]string{} // local name -> module stem
		for _, m := range rePyFromImport.FindAllStringSubmatch(f.Text, -1) {
			from := m[1][strings.LastIndexByte(m[1], '.')+1:]
			for _, part := range strings.Split(strings.Trim(m[2], "() \t\r"), ",") {
				name, alias, _ := strings.Cut(strings.TrimSpace(part), " as ")
				name, alias = strings.TrimSpace(name), strings.TrimSpace(alias)
				if alias == "" {
					alias = name
				}
				// `from app.routers import users` imports a module; `from .users import router` a name in one
				if name == "router" || name == "bp" || name == "blueprint" || strings.HasSuffix(name, "_router") || strings.HasSuffix(name, "_bp") {
					imported[alias] = from
				} else {
					imported[alias] = name
				}
			}
		}
		for _, m := range rePyInclude.FindAllStringSubmatch(f.Text, -1) {
			p := rePyPrefixKw.FindStringSubmatch(m[2])
			if p == nil {
				continue
			}
			head := strings.Split(m[1], ".")[0]
			mod := head
			if s, ok := imported[head]; ok {
				mod = s
			}
			mounts[mod] = p[1]
		}
	}
	for _, f := range files {
		if !strings.Contains(f.Text, "@") {
			continue
		}
		prefixes := map[string]string{}
		framework := map[string]string{}
		for _, m := range rePyRouterVar.FindAllStringSubmatch(f.Text, -1) {
			if p := rePyPrefixKw.FindStringSubmatch(m[3]); p != nil {
				prefixes[m[1]] = p[1]
			}
			framework[m[1]] = map[string]string{"APIRouter": "fastapi", "Blueprint": "flask"}[m[2]]
		}
		mount := mounts[stem(f.Rel)]
		if mount == "" && (stem(f.Rel) == "__init__" || stem(f.Rel) == "routes" || stem(f.Rel) == "views") {
			mount = mounts[path.Base(path.Dir(f.Rel))]
		}
		fastapi := strings.Contains(f.Text, "fastapi") || strings.Contains(f.Text, "FastAPI")
		for _, m := range rePyDecorator.FindAllStringSubmatchIndex(f.Text, -1) {
			recv := f.Text[m[2]:m[3]]
			verb := f.Text[m[4]:m[5]]
			route := f.Text[m[6]:m[7]]
			rest := f.Text[m[8]:m[9]]
			if !strings.HasPrefix(route, "/") && route != "" {
				continue // not a URL: @cache.get("key")
			}
			methods := []string{strings.ToUpper(verb)}
			if verb == "route" || verb == "api_route" {
				methods = []string{"GET"}
				if mm := rePyMethodsKw.FindStringSubmatch(rest); mm != nil {
					methods = nil
					for _, q := range rePyQuoted.FindAllStringSubmatch(mm[1], -1) {
						methods = append(methods, strings.ToUpper(q[1]))
					}
				}
			}
			fw := framework[recv]
			if fw == "" {
				fw = "flask"
				if fastapi {
					fw = "fastapi"
				}
			}
			p := pyGoPath(mount+prefixes[recv], route)
			for _, method := range methods {
				a.add(f, method, p, m[0], pyModule(f.Rel), fw)
			}
		}
	}
}

// extractDjango follows urls.py include() chains from the URLconfs nobody includes.
func extractDjango(files []*SourceFile, a *pyGoAdder) {
	type entry struct {
		pattern, include string
		at               int
	}
	confs := map[string]*SourceFile{} // dotted module -> file
	routes := map[string][]entry{}
	registered := map[string]map[string][]string{} // file -> router var -> prefixes
	included := map[string]bool{}
	for _, f := range files {
		if stem(f.Rel) != "urls" && !strings.Contains(f.Text, "urlpatterns") {
			continue
		}
		dotted := strings.ReplaceAll(strings.TrimSuffix(f.Rel, ".py"), "/", ".")
		confs[dotted] = f
		regs := map[string][]string{}
		for _, m := range reDjangoRegster.FindAllStringSubmatch(f.Text, -1) {
			regs[m[1]] = append(regs[m[1]], m[2])
		}
		registered[f.Rel] = regs
		for _, m := range reDjangoPath.FindAllStringSubmatchIndex(f.Text, -1) {
			pat := f.Text[m[4]:m[5]]
			if f.Text[m[2]:m[3]] != "path" {
				pat = djangoRegex(pat)
				if pat == "\x00" {
					continue
				}
			}
			e := entry{pattern: pat, at: m[0]}
			if m[8] >= 0 {
				e.include = f.Text[m[8]:m[9]]
				included[e.include] = true
			} else if m[10] >= 0 {
				e.include = "router:" + f.Text[m[10]:m[11]]
			}
			routes[dotted] = append(routes[dotted], e)
		}
	}
	// a dotted include may name the module from any source root: match by suffix
	resolve := func(dotted string) string {
		if _, ok := confs[dotted]; ok {
			return dotted
		}
		var hits []string
		for k := range confs {
			if strings.HasSuffix(k, "."+dotted) {
				hits = append(hits, k)
			}
		}
		sort.Strings(hits)
		if len(hits) > 0 {
			return hits[0]
		}
		return ""
	}
	includedResolved := map[string]bool{}
	for inc := range included {
		if r := resolve(inc); r != "" {
			includedResolved[r] = true
		}
	}
	var walk func(dotted, prefix string, depth int)
	walk = func(dotted, prefix string, depth int) {
		f := confs[dotted]
		if f == nil || depth > 8 {
			return
		}
		mod := pyModule(f.Rel)
		for _, e := range routes[dotted] {
			switch {
			case strings.HasPrefix(e.include, "router:"):
				for _, reg := range registered[f.Rel][strings.TrimPrefix(e.include, "router:")] {
					base := prefix + e.pattern + strings.Trim(reg, "/") + "/"
					a.add(f, "ANY", pyGoPath("", base), e.at, mod, "django")
					a.add(f, "ANY", pyGoPath("", base+"<pk>/"), e.at, mod, "django")
				}
			case e.include != "":
				walk(resolve(e.include), prefix+e.pattern, depth+1)
			default:
				a.add(f, "ANY", pyGoPath("", prefix+e.pattern), e.at, mod, "django")
			}
		}
		// a DRF router registered here and included as include(router.urls) is above;
		// one used as `urlpatterns += router.urls` has no prefix of its own
		if strings.Contains(f.Text, "router.urls") && !strings.Contains(f.Text, "include(router.urls)") {
			for _, regs := range registered[f.Rel] {
				for _, reg := range regs {
					base := prefix + strings.Trim(reg, "/") + "/"
					a.add(f, "ANY", pyGoPath("", base), 0, mod, "django")
					a.add(f, "ANY", pyGoPath("", base+"<pk>/"), 0, mod, "django")
				}
			}
		}
	}
	var roots []string
	for k := range confs {
		if !includedResolved[k] {
			roots = append(roots, k)
		}
	}
	sort.Strings(roots)
	for _, r := range roots {
		walk(r, "", 0)
	}
}

// djangoRegex turns a re_path pattern into a route, or "\x00" when it is more
// regex than route.
func djangoRegex(p string) string {
	p = strings.TrimPrefix(p, "^")
	p = strings.TrimSuffix(p, "$")
	p = reDjangoNamedX.ReplaceAllString(p, "/") // `users/?` is `users/`
	p = reDjangoNamed.ReplaceAllString(p, "<$1>")
	if strings.ContainsAny(p, `()[]*+?\|`) {
		return "\x00"
	}
	return p
}

var (
	reGoPassGroup = regexp.MustCompile(`\b([A-Z]\w*)\(\s*([A-Za-z_]\w*)(?:\.Group\(\s*"([^"]*)"\s*\))?\s*\)`)
	reGoRouterVar = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*:?=\s*(?:chi\.NewRouter|chi\.NewMux|fiber\.New)\(|\b([A-Za-z_]\w*)\s+\*?(?:chi\.Router|chi\.Mux|fiber\.Router|\*?fiber\.App)\b`)
	reGoFuncParam = regexp.MustCompile(`\bfunc\s+([A-Z]\w*)\(\s*([A-Za-z_]\w*)\s+\*?(?:gin\.RouterGroup|echo\.Group|chi\.Router|mux\.Router|fiber\.Router)\b`)
)

func extractGo(files []*SourceFile, a *pyGoAdder) {
	type scope struct {
		start, end int
		v, prefix  string
	}
	// gin and echo apps register routes in another package's function:
	// `users.UsersRegister(v1.Group("/users"))` in main, `func UsersRegister(router
	// *gin.RouterGroup)` in users/. The function's parameter carries that prefix.
	funcPrefix := map[string]string{}
	for _, f := range files {
		t := f.Text
		if !strings.Contains(t, ".Group(") {
			continue
		}
		groups := map[string]string{}
		for _, m := range reGoGroup.FindAllStringSubmatch(t, -1) {
			groups[m[1]] = groups[m[2]] + m[3]
		}
		for _, m := range reGoPassGroup.FindAllStringSubmatch(t, -1) {
			if _, known := groups[m[2]]; !known {
				continue
			}
			funcPrefix[m[1]] = groups[m[2]] + m[3]
		}
	}
	for _, f := range files {
		t := f.Text
		if !strings.Contains(t, "\"/") && !strings.Contains(t, "\"GET ") && !strings.Contains(t, "\"POST ") {
			continue
		}
		// chi: r.Route("/api", func(r chi.Router) { … }) gives the inner r a prefix
		var scopes []scope
		prefixAt := func(v string, at int) string {
			best := -1
			for i, s := range scopes {
				if s.v == v && at > s.start && at < s.end && (best < 0 || s.start > scopes[best].start) {
					best = i
				}
			}
			if best >= 0 {
				return scopes[best].prefix
			}
			return ""
		}
		groups := map[string]string{}
		for _, m := range reGoFuncParam.FindAllStringSubmatchIndex(t, -1) {
			if p, ok := funcPrefix[t[m[2]:m[3]]]; ok {
				open := strings.IndexByte(t[m[1]:], '{')
				if open >= 0 {
					open += m[1]
					scopes = append(scopes, scope{open, matchBrace(t, open), t[m[4]:m[5]], p})
				}
			}
		}
		for _, m := range reGoChiRoute.FindAllStringSubmatchIndex(t, -1) {
			open := m[1] - 1
			outer := t[m[2]:m[3]]
			p := prefixAt(outer, m[0]) + groups[outer] + t[m[4]:m[5]]
			scopes = append(scopes, scope{open, matchBrace(t, open), t[m[6]:m[7]], p})
		}
		// gin / echo / gorilla groups, in source order so a group of a group works
		for _, m := range reGoGroup.FindAllStringSubmatchIndex(t, -1) {
			parent := t[m[4]:m[5]]
			groups[t[m[2]:m[3]]] = prefixAt(parent, m[0]) + groups[parent] + t[m[6]:m[7]]
		}
		mod := path.Base(path.Dir(f.Rel))
		if mod == "." || mod == "" {
			mod = "api"
		}
		// r.Get("/x", h) is a chi or fiber route, but http.Get("/x") and
		// client.Get("/x") are requests: the title-case verbs count only in a file
		// that imports a router that uses them.
		titleRouter := strings.Contains(t, "go-chi/chi") || strings.Contains(t, "gofiber/fiber")
		routerVars := map[string]bool{}
		for _, m := range reGoRouterVar.FindAllStringSubmatch(t, -1) {
			routerVars[m[1]+m[2]] = true
		}
		for _, sc := range scopes {
			routerVars[sc.v] = true
		}
		for g := range groups {
			routerVars[g] = true
		}
		for _, m := range reGoRoute.FindAllStringSubmatchIndex(t, -1) {
			recv, verb := t[m[2]:m[3]], t[m[4]:m[5]]
			switch verb {
			case "Get", "Post", "Put", "Patch", "Delete", "Head", "Options":
				if !titleRouter || !routerVars[recv] {
					continue
				}
			}
			route := t[m[8]:m[9]]
			method := strings.ToUpper(verb)
			framework := "go"
			switch verb {
			case "Any":
				method = "ANY"
			case "Handle", "HandleFunc":
				method = "ANY"
				// Go 1.22: "GET /orders/{id}"
				if sp := strings.IndexByte(route, ' '); sp > 0 && strings.ToUpper(route[:sp]) == route[:sp] {
					method, route = route[:sp], strings.TrimSpace(route[sp+1:])
				}
				if mm := reGoMethods.FindStringSubmatch(t[m[1]:]); mm != nil {
					if q := rePyQuoted.FindStringSubmatch(strings.ReplaceAll(mm[1], `"`, `'`)); q != nil {
						method = strings.ToUpper(q[1])
					}
				}
				framework = "net/http"
			case "Method", "MethodFunc":
				if m[6] < 0 {
					continue
				}
				method = t[m[6]:m[7]]
			}
			if !strings.HasPrefix(route, "/") {
				continue
			}
			p := pyGoPath(prefixAt(recv, m[0])+groups[recv], route)
			if i := strings.Index(p, "/*"); i >= 0 {
				p = p[:i+2] // gin's *filepath, chi's /*
				p = strings.TrimSuffix(p, "*") + "*"
			}
			a.add(f, method, p, m[0], mod, framework)
		}
	}
}
