package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Spike C, fair version: the regex runs only on files that pass the same cheap
// literal prefilter the port would use. Raw throughput is meaningless without it:
// a leading \b defeats RE2's literal-prefix optimisation (~100x on this corpus).
type pat struct {
	name  string
	re    *regexp.Regexp
	scope string // code | spec | all
	pre   []string
}

var pats = []pat{
	{"IMPORT_RE", regexp.MustCompile(`(?:^|\n)\s*(?:import|export)[^'"]*?from\s*['"]([^'"]+)['"]`), "code", []string{"from"}},
	{"ROUTER_VERB", regexp.MustCompile(`router\.(get|post|put|patch|delete)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]*)['"` + "`" + `]`), "code", []string{"router"}},
	{"CALL_RE", regexp.MustCompile(`\b(?:api|http|httpClient|axios|client|fetcher)\s*\.\s*(get|post|put|patch|delete)\s*\(\s*[` + "`" + `'"]([^` + "`" + `'"]+)[` + "`" + `'"]`), "code", []string{"api", "http", "axios", "client", "fetcher"}},
	{"FETCH_RE", regexp.MustCompile(`\bfetch\s*\(\s*[` + "`" + `'"]([^` + "`" + `'"]+)[` + "`" + `'"]\s*(?:,\s*\{[^}]*method\s*:\s*['"](\w+)['"])?`), "code", []string{"fetch"}},
	{"TRPC_CALL", regexp.MustCompile(`\btrpc(?:Client|Proxy)?\s*\.\s*([\w$.]+?)\s*\.\s*(useQuery|useMutation|useInfiniteQuery|useSuspenseQuery|useQueries|query|mutate)\s*\(`), "code", []string{"trpc"}},
	{"CALLISH", regexp.MustCompile(`\bfetch\s*\(|\baxios\s*\.\s*(get|post|put|patch|delete|request)\s*\(|\buse(Query|Mutation|SWR|InfiniteQuery)\s*\(|\$fetch\s*\(|\btrpc\s*\.|\bky\s*\(|\bgot\s*\(`), "code", []string{"fetch", "axios", "useQuery", "useMutation", "useSWR", "$fetch", "trpc", "ky(", "got("}},
	{"REQ_ID", regexp.MustCompile(`\b(?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`), "spec", []string{"REQ-", "FR-"}},
	{"HEADING", regexp.MustCompile(`^(#{1,5})\s+(.*)$`), "spec", []string{"#"}},
	{"TASK_START", regexp.MustCompile(`^-\s+\[( |x|X)\]\s+(.*)$`), "spec", []string{"- ["}},
	{"TASK_ID", regexp.MustCompile(`^\**((?:[A-Z]{0,2}\d+(?:\.\d+)+)|(?:T\d+)|(?:\d+\.))\**\s+`), "spec", nil},
}

type text struct {
	body string
	code bool
	spec bool
}

type result struct {
	Pattern string  `json:"pattern"`
	Files   int     `json:"files"`
	Matches int     `json:"matches"`
	Ms      float64 `json:"ms"`
}

var codeExt = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true}
var specExt = map[string]bool{".md": true}
var allExt = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".prisma": true, ".md": true, ".json": true}
var skipDir = map[string]bool{"node_modules": true, ".git": true, ".weave": true, "dist": true, "build": true, "coverage": true}

func main() {
	root := os.Args[1]
	var texts []text
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !allExt[ext] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 1<<20 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		texts = append(texts, text{body: string(b), code: codeExt[ext], spec: specExt[ext]})
		return nil
	})
	sort.Slice(texts, func(i, j int) bool { return texts[i].body < texts[j].body })

	out := make([]result, 0, len(pats))
	for _, pp := range pats {
		inScope := 0
		best := time.Hour
		matches := 0
		for round := 0; round < 3; round++ {
			start := time.Now()
			files, n := 0, 0
			for _, t := range texts {
				if pp.scope == "code" && !t.code || pp.scope == "spec" && !t.spec {
					continue
				}
				if !ready(t.body, pp.pre) {
					continue
				}
				files++
				n += len(pp.re.FindAllStringIndex(t.body, -1))
			}
			if el := time.Since(start); el < best {
				best = el
				inScope = files
				matches = n
			}
		}
		out = append(out, result{Pattern: pp.name, Files: inScope, Matches: matches, Ms: float64(best.Microseconds()) / 1000})
	}

	// The cost of the prefilter itself, once over every in-scope file.
	start := time.Now()
	for _, t := range texts {
		ready(t.body, []string{"fetch", "axios", "useQuery", "useMutation", "useSWR", "$fetch", "trpc", "ky(", "got("})
	}
	prefMs := float64(time.Since(start).Microseconds()) / 1000

	json.NewEncoder(os.Stdout).Encode(map[string]any{"patterns": out, "prefilterMs": prefMs})
}

func ready(body string, pre []string) bool {
	if len(pre) == 0 {
		return true
	}
	for _, s := range pre {
		if strings.Contains(body, s) {
			return true
		}
	}
	return false
}
