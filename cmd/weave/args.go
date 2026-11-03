package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/michalbasisty/weave/internal/junit"
	"github.com/michalbasisty/weave/internal/registry"
	"github.com/michalbasisty/weave/internal/weave"
)

// --- argv ---------------------------------------------------------------

var valueFlags = map[string]bool{
	"--root": true, "--project": true, "--limit": true, "--budget": true,
	"--out": true, "--against": true, "--runs": true, "--file": true,
	"--note": true, "--from": true, "--since": true, "--depth": true,
	"--port": true,
}

var (
	cmd  string
	pos  []string
	root string
)

func parseArgs(argv []string) (string, []string) {
	var c string
	var p []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if valueFlags[a] {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			if c == "" && (a == "--help" || a == "-h") {
				c = "help"
			}
			if c == "" && (a == "--version" || a == "-v") {
				c = "version"
			}
			continue
		}
		if c == "" {
			c = a
		} else {
			p = append(p, a)
		}
	}
	return c, p
}

// arg is cli.ts's arg: the value after `--name`, or the fallback.
func arg(name, fallback string) string {
	for i, a := range os.Args {
		if a == "--"+name {
			if i+1 < len(os.Args) && os.Args[i+1] != "" {
				return os.Args[i+1]
			}
			break
		}
	}
	return fallback
}

func hasFlag(name string) bool {
	for _, a := range os.Args {
		if a == "--"+name {
			return true
		}
	}
	return false
}

// flagValues collects a repeatable value flag, like `--file a --file b`.
func flagValues(name string) []string {
	var out []string
	for i, a := range os.Args {
		if a == "--"+name && i+1 < len(os.Args) {
			out = append(out, os.Args[i+1])
		}
	}
	return out
}

// numberArg mirrors Number(x): invalid input is NaN, which makes the context
// budget check never fire, exactly like the TypeScript.
func numberArg(name, fallback string) float64 {
	v, err := strconv.ParseFloat(arg(name, fallback), 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

func intArg(name, fallback string) int {
	v, err := strconv.Atoi(arg(name, fallback))
	if err != nil {
		return 0
	}
	return v
}

// --- root ---------------------------------------------------------------

func pickRoot() (string, int) {
	if explicit := arg("root", ""); explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return "", 1
		}
		return abs, 0
	}
	if name := arg("project", ""); name != "" {
		if hit := registry.Resolve(name); hit != nil {
			return hit.Root, 0
		}
		near := registry.Candidates(name)
		if len(near) > 1 {
			names := make([]string, 0, len(near))
			for _, e := range near {
				names = append(names, e.Name)
			}
			fmt.Fprintf(os.Stderr, "%q matches several projects: %s\n", name, strings.Join(names, ", "))
		} else {
			fmt.Fprintf(os.Stderr, "unknown project %q. Known:\n%s\n", name, registry.Table(registry.Load()))
		}
		return "", 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return "", 1
	}
	return findRoot(cwd), 0
}

// findRoot walks up: the nearest .git/.weave wins, then the nearest weak marker.
func findRoot(start string) string {
	strong := []string{".git", ".weave"}
	weak := []string{"package.json", "go.mod", "composer.json", "pyproject.toml", "Cargo.toml", "specs"}
	dir, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	weakHit := ""
	for i := 0; i < 16; i++ {
		for _, m := range strong {
			if pathExists(filepath.Join(dir, m)) {
				return dir
			}
		}
		if weakHit == "" {
			for _, m := range weak {
				if pathExists(filepath.Join(dir, m)) {
					weakHit = dir
					break
				}
			}
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	if weakHit != "" {
		return weakHit
	}
	return start
}

func isProject(dir string) bool {
	for _, m := range []string{".git", ".weave", "package.json", "go.mod", "composer.json", "pyproject.toml", "Cargo.toml"} {
		if pathExists(filepath.Join(dir, m)) {
			return true
		}
	}
	return false
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// graphFor reads the index, rebuilding it when a source file or weave itself changed.
func graphFor() (*weave.Graph, int) {
	codeDir := ""
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			codeDir = filepath.Dir(resolved)
		} else {
			codeDir = filepath.Dir(exe)
		}
	}
	if weave.IsFresh(root, codeDir) {
		g, err := weave.LoadGraph(root)
		if err == nil {
			weave.NoteCounts(root, g)
			return g, 0
		}
	}
	g, err := weave.Reindex(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, 1
	}
	return g, 0
}

func loadCases() []junit.Case {
	p, ok := junit.FindJUnitFile(root)
	if !ok {
		return nil
	}
	text, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return junit.Parse(string(text))
}
