package weave

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Monorepos import across packages by name (`@acme/lib/server/x`) and inside an
// app through tsconfig paths (`~/components/x`, `@/lib/x`). A relative-only
// resolver sees none of those edges, so a screen whose data hook lives in another
// package "calls no endpoint". Aliases resolves both, from the repo's own files:
// every package.json with a name, and every tsconfig.json / jsconfig.json with
// compilerOptions.paths.

// Aliases is what a repository declares about its non-relative imports.
type Aliases struct {
	rules []aliasRule
	pkgs  []pkgDir // longest name first
}

type aliasRule struct {
	scope    string // the config's directory; the rule applies to files under it
	pattern  string // without the trailing `*` when wildcard
	wildcard bool
	targets  []string // repo-relative; without the trailing `*` when wildcard
}

type pkgDir struct{ name, dir string }

var (
	reJSONLineComment  = regexp.MustCompile(`(?m)^\s*//.*$`)
	reJSONBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reJSONTrailComma   = regexp.MustCompile(`,(\s*[}\]])`)
)

// parseJSONC reads the JSON-with-comments that tsconfig files are written in.
func parseJSONC(text string, v any) error {
	s := reJSONBlockComment.ReplaceAllString(text, "")
	s = reJSONLineComment.ReplaceAllString(s, "")
	s = reJSONTrailComma.ReplaceAllString(s, "$1")
	return json.Unmarshal([]byte(s), v)
}

// LoadAliases reads the package names and path aliases a repository declares.
func LoadAliases(files []SourceFile) *Aliases {
	a := &Aliases{}
	for i := range files {
		f := &files[i]
		base := f.Rel[strings.LastIndexByte(f.Rel, '/')+1:]
		dir := DirOf(f.Rel)
		switch {
		case base == "package.json":
			var p struct {
				Name string `json:"name"`
			}
			if json.Unmarshal([]byte(f.Text), &p) == nil && p.Name != "" && dir != "" {
				a.pkgs = append(a.pkgs, pkgDir{p.Name, dir})
			}
		case base == "tsconfig.json" || base == "jsconfig.json":
			var c struct {
				CompilerOptions struct {
					BaseURL string              `json:"baseUrl"`
					Paths   map[string][]string `json:"paths"`
				} `json:"compilerOptions"`
			}
			if parseJSONC(f.Text, &c) != nil || len(c.CompilerOptions.Paths) == 0 {
				continue
			}
			baseURL := JoinPosix(dir, c.CompilerOptions.BaseURL)
			for pat, targets := range c.CompilerOptions.Paths {
				r := aliasRule{scope: dir, pattern: pat}
				if strings.HasSuffix(pat, "*") {
					r.wildcard = true
					r.pattern = strings.TrimSuffix(pat, "*")
				}
				for _, t := range targets {
					star := strings.HasSuffix(t, "*")
					t = JoinPosix(baseURL, strings.TrimSuffix(t, "*"))
					if star && t != "" {
						t += "/"
					}
					r.targets = append(r.targets, t)
				}
				a.rules = append(a.rules, r)
			}
		}
	}
	// Deepest config first, then the longest pattern: the most specific rule wins.
	sort.SliceStable(a.rules, func(i, j int) bool {
		if len(a.rules[i].scope) != len(a.rules[j].scope) {
			return len(a.rules[i].scope) > len(a.rules[j].scope)
		}
		return len(a.rules[i].pattern) > len(a.rules[j].pattern)
	})
	sort.SliceStable(a.pkgs, func(i, j int) bool { return len(a.pkgs[i].name) > len(a.pkgs[j].name) })
	return a
}

// codeExts are the extensions an import may name and still mean a source file.
var codeExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"}

// stripCodeExt drops a code extension only: `./find-teams.types` keeps its name.
func stripCodeExt(p string) string {
	for _, e := range codeExts {
		if strings.HasSuffix(p, e) {
			return strings.TrimSuffix(p, e)
		}
	}
	return p
}

// fileFor is the source file an extensionless import base names, or "".
func fileFor(base string, known map[string]bool) string {
	base = strings.TrimSuffix(base, "/")
	for _, g := range []string{
		base, base + ".ts", base + ".tsx", base + ".js", base + ".jsx", base + ".mjs",
		base + "/index.ts", base + "/index.tsx", base + "/index.js", base + "/index.jsx",
	} {
		if known[g] && g != "" {
			return g
		}
	}
	return ""
}

// Resolve maps an import spec to a file in the repository, or "".
func (a *Aliases) Resolve(importer, spec string, known map[string]bool) string {
	if strings.HasPrefix(spec, ".") {
		return ResolveImport(importer, spec, known)
	}
	if a == nil || spec == "" {
		return ""
	}
	for _, r := range a.rules {
		if r.scope != "" && !strings.HasPrefix(importer, r.scope+"/") {
			continue
		}
		var rest string
		switch {
		case r.wildcard && strings.HasPrefix(spec, r.pattern):
			rest = spec[len(r.pattern):]
		case !r.wildcard && spec == r.pattern:
		default:
			continue
		}
		for _, t := range r.targets {
			if hit := a.resolveIn(t+rest, known); hit != "" {
				return hit
			}
		}
	}
	for _, p := range a.pkgs {
		if spec != p.name && !strings.HasPrefix(spec, p.name+"/") {
			continue
		}
		sub := strings.TrimPrefix(strings.TrimPrefix(spec, p.name), "/")
		for _, base := range []string{JoinPosix(p.dir, sub), JoinPosix(p.dir, JoinPosix("src", sub))} {
			if hit := a.resolveIn(base, known); hit != "" {
				return hit
			}
		}
		return ""
	}
	return ""
}

func (a *Aliases) resolveIn(base string, known map[string]bool) string {
	if hit := fileFor(base, known); hit != "" {
		return hit
	}
	return fileFor(stripCodeExt(base), known)
}
