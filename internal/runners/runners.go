// Package runners is the Go port of src/runners.ts: detect the test runner a
// package already uses and hand back the command it already understands.
package runners

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// RunPlan is the commands to run and the files no runner claimed.
type RunPlan struct {
	Commands []string
	Files    []string
}

type pkg struct {
	deps    map[string]bool
	scripts map[string]string
}

type cachedPkg struct {
	pkg *pkg
	ok  bool
}

type group struct {
	dir   string
	tool  string
	files []string
}

// Plan picks a runner per package and groups test files by directory and tool.
// At most four commands are returned; ungrouped files are returned as they are.
func Plan(root string, testFiles []string) RunPlan {
	cache := map[string]cachedPkg{}

	pkgAt := func(dir string) *pkg {
		if c, ok := cache[dir]; ok {
			return c.pkg
		}
		c := cachedPkg{ok: true}
		p := filepath.Join(root, dir, "package.json")
		if raw, err := os.ReadFile(p); err == nil {
			if v, err := jsjson.Parse([]byte(jsstr.DecodeUTF8(raw))); err == nil {
				if o := v.Object(); o != nil {
					c.pkg = parsePkg(o)
				}
			}
		}
		cache[dir] = c
		return c.pkg
	}

	nearestPkg := func(rel string) (string, *pkg, bool) {
		dir := jsDirname(strings.ReplaceAll(rel, "\\", "/"))
		for i := 0; i < 8; i++ {
			if p := pkgAt(dir); p != nil {
				return dir, p, true
			}
			if dir == "." || dir == "" || dir == "/" {
				return "", nil, false
			}
			up := jsDirname(dir)
			if up == dir {
				return "", nil, false
			}
			dir = up
		}
		return "", nil, false
	}

	groups := make([]*group, 0)
	index := map[string]int{}
	add := func(dir, tool, file string) {
		key := dir + "|" + tool
		if i, ok := index[key]; ok {
			groups[i].files = append(groups[i].files, file)
			return
		}
		index[key] = len(groups)
		groups = append(groups, &group{dir: dir, tool: tool, files: []string{file}})
	}

	ungrouped := make([]string, 0)
	for _, file := range testFiles {
		dir, p, found := nearestPkg(file)
		if !found {
			if exists(filepath.Join(root, "vendor/bin/phpunit")) || exists(filepath.Join(root, "phpunit.xml")) {
				add(".", "phpunit", file)
			} else if exists(filepath.Join(root, "go.mod")) {
				add(".", "go", file)
			} else {
				ungrouped = append(ungrouped, file)
			}
			continue
		}
		rel := file
		if dir != "." {
			rel = jsstr.Slice(file, jsstr.Len(dir)+1, jsstr.Len(file))
		}
		add(dir, pickTool(root, dir, p, file), rel)
	}

	commands := make([]string, 0, len(groups))
	for _, g := range groups {
		list := strings.Join(g.files, " ")
		var cmd string
		switch g.tool {
		case "vitest":
			cmd = "npx vitest run " + list
		case "jest":
			cmd = "npx jest " + list
		case "playwright":
			cmd = "npx playwright test " + list
		case "phpunit":
			cmd = "vendor/bin/phpunit " + list
		case "go":
			cmd = "go test ./" + strings.Join(uniqueDirs(g.files), " ./") + "/..."
		default:
			cmd = "# run " + list
		}
		if g.dir == "." || g.dir == "" {
			commands = append(commands, cmd)
		} else {
			commands = append(commands, "cd "+g.dir+" && "+cmd)
		}
	}
	if len(commands) > 4 {
		commands = commands[:4]
	}
	return RunPlan{Commands: commands, Files: ungrouped}
}

func parsePkg(o *jsjson.Object) *pkg {
	p := &pkg{deps: map[string]bool{}, scripts: map[string]string{}}
	for _, field := range []string{"dependencies", "devDependencies"} {
		v, ok := o.Get(field)
		if !ok {
			continue
		}
		deps := v.Object()
		if deps == nil {
			continue
		}
		for _, name := range deps.Keys() {
			p.deps[name] = true
		}
	}
	if v, ok := o.Get("scripts"); ok {
		if scripts := v.Object(); scripts != nil {
			for _, name := range scripts.Keys() {
				value, _ := scripts.Get(name)
				p.scripts[name] = value.Str()
			}
		}
	}
	return p
}

func pickTool(root, dir string, p *pkg, file string) string {
	has := func(names ...string) bool {
		for _, name := range names {
			if exists(filepath.Join(root, dir, name)) {
				return true
			}
		}
		return false
	}
	e2e := strings.HasPrefix(file, "e2e/") || strings.Contains(file, "/e2e/") ||
		strings.HasSuffix(file, ".e2e.ts") || strings.HasSuffix(file, ".e2e.js")
	if p.deps["@playwright/test"] && e2e {
		return "playwright"
	}
	// A jest config next to a vite config means the unit tests run under jest.
	if has("jest.config.js", "jest.config.ts", "jest.config.mjs", "jest.config.cjs") {
		return "jest"
	}
	if has("vitest.config.ts", "vitest.config.js", "vitest.config.mts", "vite.config.ts") {
		return "vitest"
	}
	if p.deps["vitest"] {
		return "vitest"
	}
	if p.deps["jest"] {
		return "jest"
	}
	if p.deps["@playwright/test"] {
		return "playwright"
	}
	if strings.Contains(p.scripts["test"], "vitest") {
		return "vitest"
	}
	if strings.Contains(p.scripts["test"], "jest") {
		return "jest"
	}
	return "unknown"
}

func uniqueDirs(files []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(files))
	for _, f := range files {
		dir := jsDirname(strings.ReplaceAll(f, "\\", "/"))
		if dir == "." {
			dir = ""
		}
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// jsDirname is path.dirname on the registry's slash-normalized paths. Unlike
// filepath.Dir it drops trailing separators, as Node does: "a/b/" -> "a".
func jsDirname(p string) string {
	if p == "" {
		return "."
	}
	end := len(p)
	for end > 1 && p[end-1] == '/' {
		end--
	}
	p = p[:end]
	if p == "/" {
		return "/"
	}
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "."
	}
	if i == 0 {
		return "/"
	}
	return p[:i]
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
