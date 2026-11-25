package weave

import (
	"path"
	"regexp"
	"strings"
)

// Rails and Laravel declare routes in one file and serve them from controllers:
//
//	resources :articles, param: :slug do            # config/routes.rb
//	  resource :favorite, only: [:create, :destroy]
//	end
//	Route::resource('articles', 'ArticleController'); // routes/api.php
//
// Each route becomes an endpoint defined at the controller action when that file
// and method exist (so context(controller) lists them), otherwise at the route line.

func isRuby(rel string) bool { return strings.HasSuffix(rel, ".rb") }
func isPHP(rel string) bool  { return strings.HasSuffix(rel, ".php") }

// singular is the inverse of a plain English plural, enough for route names.
func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ses"), strings.HasSuffix(s, "xes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return s[:len(s)-1]
	}
	return s
}

// ExtractRubyPHP reads Rails config/routes.rb and Laravel routes/*.php.
func ExtractRubyPHP(files []SourceFile, b *Builder) []Endpoint {
	a := &pyGoAdder{b: b, seen: map[string]bool{}, mods: map[string]bool{}}
	byRel := map[string]*SourceFile{}
	for i := range files {
		byRel[files[i].Rel] = &files[i]
	}
	for i := range files {
		f := &files[i]
		switch {
		case isRuby(f.Rel) && (strings.HasSuffix(f.Rel, "config/routes.rb") || strings.Contains(f.Rel, "config/routes/")):
			railsRoutes(a, f, byRel)
		case isPHP(f.Rel) && (strings.HasPrefix(f.Rel, "routes/") || strings.Contains(f.Rel, "/routes/")) && strings.Contains(f.Text, "Route::"):
			laravelRoutes(a, f, byRel)
		}
	}
	return a.out
}

// ---------------------------------------------------------------- rails

var (
	reRbSym      = regexp.MustCompile(`^:?["']?([\w/.\-]+)["']?`)
	reRbOpt      = func(k string) *regexp.Regexp { return regexp.MustCompile(`\b` + k + `:\s*(\[[^\]]*\]|:?["']?[\w/\-]+["']?)`) }
	reRbOnly     = reRbOpt("only")
	reRbExcept   = reRbOpt("except")
	reRbParam    = reRbOpt("param")
	reRbPath     = reRbOpt("path")
	reRbOn       = reRbOpt("on")
	reRbVia      = reRbOpt("via")
	reRbTo       = regexp.MustCompile(`(?:\bto:\s*|=>\s*)["']([\w/]+)#(\w+)["']`)
	reRbCtrl     = reRbOpt("controller")
	reRbSignIn   = regexp.MustCompile(`sign_in:\s*:?["']?(\w+)`)
	reRbLineHead = regexp.MustCompile(`^(resources|resource|namespace|scope|member|collection|get|post|put|patch|delete|match|root|devise_for)\b\s*\(?\s*(.*)$`)
)

func rbList(re *regexp.Regexp, s string) []string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	var out []string
	for _, w := range regexp.MustCompile(`[\w]+`).FindAllString(m[1], -1) {
		out = append(out, w)
	}
	return out
}

func rbValue(re *regexp.Regexp, s string) string {
	if v := rbList(re, s); len(v) > 0 {
		return v[0]
	}
	return ""
}

type rbFrame struct {
	prefix string // URL prefix for routes inside the block
	ns     string // controller namespace (admin/)
	ctrl   string // controller for verbs inside a resources block
	member string // member prefix of the enclosing resources, for member do
	base   string // collection prefix of the enclosing resources
	opens  bool   // this frame came from a do … end
}

var railsActions = []struct{ action, method, at string }{
	{"index", "GET", "base"}, {"create", "POST", "base"}, {"new", "GET", "base/new"},
	{"show", "GET", "member"}, {"update", "PATCH", "member"}, {"update", "PUT", "member"},
	{"destroy", "DELETE", "member"}, {"edit", "GET", "member/edit"},
}

func railsRoutes(a *pyGoAdder, f *SourceFile, byRel map[string]*SourceFile) {
	stack := []rbFrame{{}}
	root := strings.TrimSuffix(f.Rel[:strings.Index(f.Rel, "config/routes")], "/")
	if root != "" {
		root += "/"
	}
	lines := strings.Split(f.Text, "\n")
	off := 0
	for k := 0; k < len(lines); k++ {
		raw := lines[k]
		at := off
		off += len(raw) + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// a call whose options run onto the next line: devise_for :users, controllers: …,
		for strings.HasSuffix(line, ",") && k+1 < len(lines) {
			k++
			off += len(lines[k]) + 1
			line += " " + strings.TrimSpace(lines[k])
		}
		top := stack[len(stack)-1]
		if line == "end" || strings.HasPrefix(line, "end ") || line == "}" {
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		opens := strings.HasSuffix(line, " do") || strings.Contains(line, " do |") || strings.HasSuffix(line, "{")
		m := reRbLineHead.FindStringSubmatch(line)
		if m == nil {
			if opens { // a block we do not read, such as constraints or concern
				stack = append(stack, rbFrame{top.prefix, top.ns, top.ctrl, top.member, top.base, true})
			}
			continue
		}
		kw, rest := m[1], m[2]
		name := ""
		if s := reRbSym.FindStringSubmatch(rest); s != nil {
			name = s[1]
		}
		next := top
		next.opens = true
		emit := func(method, p, ctrl, action string) {
			railsEmit(a, f, byRel, at, method, p, root, top.ns, ctrl, action)
		}
		switch kw {
		case "scope":
			p := rbValue(reRbPath, rest)
			if p == "" && !strings.Contains(rest, "module:") && !strings.HasPrefix(rest, "path") {
				p = name
			}
			if p != "" {
				next.prefix = top.prefix + "/" + strings.Trim(p, "/")
			}
			if mod := rbValue(reRbOpt("module"), rest); mod != "" {
				next.ns = top.ns + mod + "/"
			}
		case "namespace":
			next.prefix = top.prefix + "/" + name
			next.ns = top.ns + name + "/"
		case "resources", "resource":
			plural := kw == "resources"
			seg := name
			if p := rbValue(reRbPath, rest); p != "" {
				seg = p
			}
			base := top.prefix + "/" + seg
			member := base
			ctrl := name
			if !plural {
				ctrl = name + "s"
			}
			if c := rbValue(reRbCtrl, rest); c != "" {
				ctrl = c
			}
			param := "id"
			if p := rbValue(reRbParam, rest); p != "" {
				param = p
			}
			if plural {
				member = base + "/:" + param
			}
			only, except := rbList(reRbOnly, rest), rbList(reRbExcept, rest)
			keep := func(act string) bool {
				if only != nil {
					return contains(only, act)
				}
				return !contains(except, act)
			}
			for _, r := range railsActions {
				if !keep(r.action) || !plural && r.action == "index" {
					continue
				}
				p := strings.Replace(strings.Replace(r.at, "member", member, 1), "base", base, 1)
				emit(r.method, p, ctrl, r.action)
			}
			next.ctrl, next.base, next.member = ctrl, base, member
			next.prefix = base
			if plural {
				nested := "id"
				if param != "id" {
					nested = param
				}
				next.prefix = base + "/:" + singular(name) + "_" + nested
			}
		case "member":
			next.prefix = top.member
		case "collection":
			next.prefix = top.base
		case "root":
			ctrl, action := top.ctrl, "index"
			if t := reRbTo.FindStringSubmatch(rest); t != nil {
				ctrl, action = t[1], t[2]
			} else if s := regexp.MustCompile(`["']([\w/]+)#(\w+)["']`).FindStringSubmatch(rest); s != nil {
				ctrl, action = s[1], s[2]
			}
			emit("GET", top.prefix+"/", ctrl, action)
		case "devise_for":
			signIn := "sign_in"
			if s := reRbSignIn.FindStringSubmatch(rest); s != nil {
				signIn = s[1]
			}
			base := top.prefix + "/" + name
			sessions, registrations := "devise/sessions", "devise/registrations"
			if c := regexp.MustCompile(`sessions:\s*:?["']?([\w/]+)`).FindStringSubmatch(rest); c != nil {
				sessions = c[1]
			}
			if c := regexp.MustCompile(`registrations:\s*:?["']?([\w/]+)`).FindStringSubmatch(rest); c != nil {
				registrations = c[1]
			}
			emit("POST", base+"/"+signIn, sessions, "create")
			emit("DELETE", base+"/sign_out", sessions, "destroy")
			emit("POST", base, registrations, "create")
			emit("PUT", base, registrations, "update")
			emit("PATCH", base, registrations, "update")
		default: // get post put patch delete match
			if name == "" {
				break
			}
			prefix := top.prefix
			switch rbValue(reRbOn, rest) {
			case "collection":
				prefix = top.base
			case "member":
				prefix = top.member
			}
			p := prefix + "/" + strings.TrimLeft(name, "/")
			ctrl, action := top.ctrl, path.Base(name)
			if t := reRbTo.FindStringSubmatch(rest); t != nil {
				ctrl, action = t[1], t[2]
			}
			methods := []string{strings.ToUpper(kw)}
			if kw == "match" {
				methods = nil
				for _, v := range rbList(reRbVia, rest) {
					methods = append(methods, strings.ToUpper(v))
				}
				if len(methods) == 0 {
					methods = []string{"ANY"}
				}
			}
			for _, meth := range methods {
				emit(meth, p, ctrl, action)
			}
		}
		if opens {
			stack = append(stack, next)
		}
	}
}

// railsEmit defines the endpoint at app/controllers/<ns><ctrl>_controller.rb's
// `def action` when it exists, otherwise at the routes line.
func railsEmit(a *pyGoAdder, f *SourceFile, byRel map[string]*SourceFile, at int, method, p, root, ns, ctrl, action string) {
	p = pyGoPath("", p)
	mod := path.Base(ctrl)
	if mod == "" || mod == "." {
		mod = "routes"
	}
	if cf := byRel[root+"app/controllers/"+ns+ctrl+"_controller.rb"]; cf != nil {
		if loc := regexp.MustCompile(`(?m)^\s*def\s+` + regexp.QuoteMeta(action) + `\b`).FindStringIndex(cf.Text); loc != nil {
			a.add(cf, method, p, loc[0], mod, "rails")
			return
		}
		a.add(cf, method, p, 0, mod, "rails")
		return
	}
	a.add(f, method, p, at, mod, "rails")
}

// ---------------------------------------------------------------- laravel

var (
	reLvVerb     = regexp.MustCompile(`Route::(get|post|put|patch|delete|options|any)\(\s*['"]([^'"]*)['"]\s*,\s*([^;]*)`)
	reLvMatch    = regexp.MustCompile(`Route::match\(\s*\[([^\]]*)\]\s*,\s*['"]([^'"]*)['"]\s*,\s*([^;]*)`)
	reLvResource = regexp.MustCompile(`Route::(resource|apiResource)\(\s*['"]([^'"]+)['"]\s*,\s*([^;]*)`)
	reLvGroup    = regexp.MustCompile(`Route::(?:group\(\s*\[([^\]]*)\]\s*,|(?:prefix|middleware|namespace|name|controller)\([^)]*\)(?:->\w+\([^)]*\))*->group\()\s*function\s*\([^)]*\)\s*(?:use\s*\([^)]*\)\s*)?\{`)
	reLvPrefix   = regexp.MustCompile(`prefix['"]?\s*(?:=>|\()\s*['"]([^'"]*)['"]`)
	reLvNs       = regexp.MustCompile(`namespace['"]?\s*(?:=>|\()\s*['"]([^'"]*)['"]`)
	reLvAction   = regexp.MustCompile(`['"]([\w\\]+)@(\w+)['"]|\[\s*([\w\\]+)::class\s*,\s*['"](\w+)['"]\s*\]`)
	reLvCtrlArg  = regexp.MustCompile(`^\s*['"]?([\w\\]+?)(?:::class)?['"]?\s*[,)]`)
	reLvOnly     = regexp.MustCompile(`['"]only['"]\s*=>\s*\[([^\]]*)\]|->only\(\s*\[([^\]]*)\]`)
	reLvExcept   = regexp.MustCompile(`['"]except['"]\s*=>\s*\[([^\]]*)\]|->except\(\s*\[([^\]]*)\]`)
	reLvWords    = regexp.MustCompile(`\w+`)
	reLvOptional = regexp.MustCompile(`\{(\w+)\?\}`)
)

var laravelActions = []struct{ action, method, at string }{
	{"index", "GET", "base"}, {"create", "GET", "base/create"}, {"store", "POST", "base"},
	{"show", "GET", "member"}, {"edit", "GET", "member/edit"}, {"update", "PUT", "member"},
	{"update", "PATCH", "member"}, {"destroy", "DELETE", "member"},
}

func laravelRoutes(a *pyGoAdder, f *SourceFile, byRel map[string]*SourceFile) {
	t := phpCode(f.Text)
	root := ""
	if i := strings.LastIndex(f.Rel, "routes/"); i > 0 {
		root = f.Rel[:i]
	}
	// routes/api.php is mounted under /api by Laravel's RouteServiceProvider
	filePrefix := ""
	if path.Base(f.Rel) == "api.php" {
		filePrefix = "/api"
	}
	type scope struct {
		open, close int
		prefix, ns  string
	}
	var scopes []scope
	for _, m := range reLvGroup.FindAllStringSubmatchIndex(t, -1) {
		head := t[m[0]:m[1]]
		open := m[1] - 1
		s := scope{open: open, close: matchBrace(t, open)}
		if p := reLvPrefix.FindStringSubmatch(head); p != nil {
			s.prefix = p[1]
		}
		if n := reLvNs.FindStringSubmatch(head); n != nil {
			s.ns = n[1]
		}
		scopes = append(scopes, s)
	}
	at := func(pos int) (string, string) {
		pre, ns := filePrefix, ""
		for _, s := range scopes { // outer groups come first in source order
			if pos > s.open && pos < s.close {
				if s.prefix != "" {
					pre += "/" + strings.Trim(s.prefix, "/")
				}
				if s.ns != "" {
					ns += strings.Trim(s.ns, `\`) + `\`
				}
			}
		}
		return pre, ns
	}
	emit := func(pos int, method, p, ns, ctrl, action string) {
		p = pyGoPath("", reLvOptional.ReplaceAllString(p, "{$1}"))
		laravelEmit(a, f, byRel, pos, method, p, root, ns, ctrl, action)
	}
	for _, m := range reLvVerb.FindAllStringSubmatchIndex(t, -1) {
		pre, ns := at(m[0])
		ctrl, action := lvAction(t[m[6]:m[7]])
		emit(m[0], strings.ToUpper(t[m[2]:m[3]]), pre+"/"+t[m[4]:m[5]], ns, ctrl, action)
	}
	for _, m := range reLvMatch.FindAllStringSubmatchIndex(t, -1) {
		pre, ns := at(m[0])
		ctrl, action := lvAction(t[m[6]:m[7]])
		for _, v := range reLvWords.FindAllString(t[m[2]:m[3]], -1) {
			emit(m[0], strings.ToUpper(v), pre+"/"+t[m[4]:m[5]], ns, ctrl, action)
		}
	}
	for _, m := range reLvResource.FindAllStringSubmatchIndex(t, -1) {
		pre, ns := at(m[0])
		name, args := t[m[4]:m[5]], t[m[6]:m[7]]
		ctrl := ""
		if c := reLvCtrlArg.FindStringSubmatch(args); c != nil {
			ctrl = c[1]
		}
		segs := strings.Split(strings.Trim(name, "/"), "/")
		last := segs[len(segs)-1]
		base := pre + "/" + strings.Trim(name, "/")
		member := base + "/{" + singular(last) + "}"
		only, except := lvList(reLvOnly, args), lvList(reLvExcept, args)
		for _, r := range laravelActions {
			if t[m[2]:m[3]] == "apiResource" && (r.action == "create" || r.action == "edit") {
				continue
			}
			if only != nil && !contains(only, r.action) || contains(except, r.action) {
				continue
			}
			p := strings.Replace(strings.Replace(r.at, "member", member, 1), "base", base, 1)
			emit(m[0], r.method, p, ns, ctrl, r.action)
		}
	}
}

func lvList(re *regexp.Regexp, s string) []string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	return reLvWords.FindAllString(m[1]+m[2], -1)
}

func lvAction(args string) (string, string) {
	if m := reLvAction.FindStringSubmatch(args); m != nil {
		if m[1] != "" {
			return m[1], m[2]
		}
		return m[3], m[4]
	}
	return "", ""
}

// laravelEmit defines the endpoint at app/Http/Controllers/<ns><Ctrl>.php's
// `function action` when it exists, otherwise at the route line.
func laravelEmit(a *pyGoAdder, f *SourceFile, byRel map[string]*SourceFile, at int, method, p, root, ns, ctrl, action string) {
	short := ctrl
	if i := strings.LastIndex(short, `\`); i >= 0 {
		short = short[i+1:]
	}
	mod := strings.ToLower(strings.TrimSuffix(short, "Controller"))
	if mod == "" {
		mod = "routes"
	}
	if ctrl != "" {
		rel := ctrl
		if !strings.Contains(rel, `\`) {
			rel = ns + rel
		}
		rel = strings.TrimPrefix(strings.ReplaceAll(rel, `\`, "/"), "App/Http/Controllers/")
		if cf := byRel[root+"app/Http/Controllers/"+rel+".php"]; cf != nil {
			pos := 0
			if action != "" {
				if loc := regexp.MustCompile(`function\s+` + regexp.QuoteMeta(action) + `\s*\(`).FindStringIndex(cf.Text); loc != nil {
					pos = loc[0]
				}
			}
			a.add(cf, method, p, pos, mod, "laravel")
			return
		}
	}
	a.add(f, method, p, at, mod, "laravel")
}

// phpCode blanks // and /* */ comments, keeping offsets, so commented-out routes
// are not read.
func phpCode(t string) string {
	b := []byte(t)
	inStr := byte(0)
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case inStr != 0:
			if c == '\\' {
				i++
			} else if c == inStr {
				inStr = 0
			}
		case c == '\'' || c == '"':
			inStr = c
		case c == '/' && i+1 < len(b) && b[i+1] == '/', c == '#':
			for ; i < len(b) && b[i] != '\n'; i++ {
				b[i] = ' '
			}
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			for ; i < len(b) && !(b[i] == '*' && i+1 < len(b) && b[i+1] == '/'); i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
			if i+1 < len(b) {
				b[i], b[i+1] = ' ', ' '
				i++
			}
		}
	}
	return string(b)
}
