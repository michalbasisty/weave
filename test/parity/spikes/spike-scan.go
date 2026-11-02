package main

// Spike C2: the hot extractors as hand scanners instead of regexes.
// A leading \b in Go's regexp defeats RE2's literal-prefix optimisation: on this
// 9.4 MB corpus `\bfetch` costs ~224 ms while `strings.Index("fetch")` costs ~1 ms.
// These scanners reproduce the four hot shapes and are timed against the Node
// numbers from spike-regex.mjs. Counts must be refined to exact parity against the
// edge corpus before they replace the regexes in the port.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var codeExt = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true}
var specExt = map[string]bool{".md": true}
var skipDir = map[string]bool{"node_modules": true, ".git": true, ".weave": true, "dist": true, "build": true, "coverage": true}

func isIdent(c byte) bool {
	return c == '_' || c == '$' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' }
func atQuote(t string, k int) bool {
	if k >= len(t) {
		return false
	}
	return t[k] == '\'' || t[k] == '"' || t[k] == '`'
}

func skipSpace(t string, k int) int {
	for k < len(t) && isSpace(t[k]) {
		k++
	}
	return k
}

// api.get('/x'), client . post(`/y`) — the regex's CALL_RE.
func calls(t string) int {
	strs := map[string]bool{"api": true, "http": true, "httpClient": true, "axios": true, "client": true, "fetcher": true}
	n := 0
	for _, m := range []string{"get(", "post(", "put(", "patch(", "delete("} {
		from := 0
		for {
			j := strings.Index(t[from:], m)
			if j < 0 {
				break
			}
			pos := from + j
			if atQuote(t, skipSpace(t, pos+len(m))) {
				k := pos
				for k > 0 && isSpace(t[k-1]) {
					k--
				}
				if k > 0 && t[k-1] == '.' {
					k = skipSpaceBack(t, k-1)
					end := k
					for k > 0 && isIdent(t[k-1]) {
						k--
					}
					if strs[t[k:end]] {
						n++
					}
				}
			}
			from = pos + 1
		}
	}
	return n
}

func skipSpaceBack(t string, k int) int {
	for k > 0 && isSpace(t[k-1]) {
		k--
	}
	return k
}

// fetch('/x'), fetch(`/y`, { method: 'POST' }) — the regex's FETCH_RE.
func fetches(t string) int {
	n, from := 0, 0
	for {
		j := strings.Index(t[from:], "fetch")
		if j < 0 {
			break
		}
		pos := from + j
		if (pos == 0 || !isIdent(t[pos-1])) && atQuote(t, skipSpace(t, skipParen(t, pos+5))) {
			n++
		}
		from = pos + 5
	}
	return n
}

func skipParen(t string, k int) int {
	k = skipSpace(t, k)
	if k < len(t) && t[k] == '(' {
		return k + 1
	}
	return len(t)
}

// trpc.quotes.list.useQuery() — the regex's TRPC_CALL.
func trpcs(t string) int {
	n, from := 0, 0
	for {
		j := strings.Index(t[from:], "trpc")
		if j < 0 {
			break
		}
		pos := from + j
		if pos == 0 || !isIdent(t[pos-1]) {
			k := pos + 4
			if strings.HasPrefix(t[k:], "Client") {
				k += 6
			} else if strings.HasPrefix(t[k:], "Proxy") {
				k += 5
			}
			if k = skipSpace(t, k); k < len(t) && t[k] == '.' {
				n++
			}
		}
		from = pos + 4
	}
	return n
}

// REQ-ORD-1, FR-3, NFR-2 — the regex's REQ_ID.
func reqs(t string) int {
	n := 0
	for _, pre := range []string{"REQ-", "NFR-", "FR-"} {
		from := 0
		for {
			j := strings.Index(t[from:], pre)
			if j < 0 {
				break
			}
			pos := from + j
			okBoundary := pos == 0 || !isIdent(t[pos-1])
			if pre == "FR-" && pos >= 2 && t[pos-2:pos] == "NF" {
				okBoundary = false // already counted as NFR-
			}
			k := pos + len(pre)
			ok := okBoundary && k < len(t) && (isUpper(t[k]) || (t[k] >= '0' && t[k] <= '9'))
			for ok && k < len(t) {
				c := t[k]
				if !isUpper(c) && !(c >= '0' && c <= '9') && c != '-' {
					break
				}
				k++
			}
			if ok {
				n++
			}
			from = pos + len(pre)
		}
	}
	return n
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }

type row struct {
	Name    string  `json:"name"`
	Matches int     `json:"matches"`
	Ms      float64 `json:"ms"`
}

func main() {
	root := os.Args[1]
	var code, spec []string
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
		if !codeExt[ext] && !specExt[ext] {
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
		if codeExt[ext] {
			code = append(code, string(b))
		} else {
			spec = append(spec, string(b))
		}
		return nil
	})
	sort.Strings(code)
	sort.Strings(spec)

	out := []row{}
	for _, s := range []struct {
		name string
		on   []string
		fn   func(string) int
	}{
		{"calls", code, calls},
		{"fetch", code, fetches},
		{"trpc", code, trpcs},
		{"req", spec, reqs},
	} {
		best := time.Hour
		var n int
		for i := 0; i < 3; i++ {
			t0 := time.Now()
			c := 0
			for _, t := range s.on {
				c += s.fn(t)
			}
			if el := time.Since(t0); el < best {
				best = el
				n = c
			}
		}
		out = append(out, row{s.name, n, float64(best.Microseconds()) / 1000})
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"patterns": out})
	fmt.Fprintln(os.Stderr, "hand scanners over", len(code), "code files and", len(spec), "spec files")
}
