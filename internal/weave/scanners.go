package weave

import "strings"

// Hand scanners for the patterns RE2 runs ~100x slower than V8 because they start
// with \b or match nearly every file (docs/GO-MIGRATION.md finding 3). Each one
// reproduces the JavaScript regex it names, including backtracking, and finds the
// same matches in the same order.

// importSpecs is every capture of
// /(?:^|\n)\s*(?:import|export)[^'"]*?from\s*['"]([^'"]+)['"]/g.
func importSpecs(t string) []string {
	var out []string
	from := 0
	try := func(k int) (spec string, end int, ok bool) {
		k = skipSpace(t, k)
		if !strings.HasPrefix(t[k:], "import") && !strings.HasPrefix(t[k:], "export") {
			return "", 0, false
		}
		k += 6
		// [^'"]*? stops at the first quote; "from" must start before it.
		window := t[k:]
		if q := strings.IndexAny(window, `'"`); q >= 0 {
			window = window[:q]
		}
		for at := 0; ; {
			j := strings.Index(window[at:], "from")
			if j < 0 {
				return "", 0, false
			}
			r := skipSpace(t, k+at+j+4)
			if r < len(t) && isQuote2(t[r]) {
				s := r + 1
				e := s
				for e < len(t) && !isQuote2(t[e]) {
					e++
				}
				if e > s && e < len(t) {
					return t[s:e], e + 1, true
				}
			}
			at += j + 1
		}
	}
	if spec, end, ok := try(0); ok {
		out = append(out, spec)
		from = end
	}
	for from < len(t) {
		j := strings.IndexByte(t[from:], '\n')
		if j < 0 {
			break
		}
		nl := from + j
		if spec, end, ok := try(nl + 1); ok {
			out = append(out, spec)
			from = end
			continue
		}
		from = nl + 1
	}
	return out
}

var callClients = map[string]bool{"api": true, "http": true, "httpClient": true, "axios": true, "client": true, "fetcher": true}

// verbAt is the HTTP verb of (get|post|put|patch|delete) at s[k], or "".
func verbAt(s string, k int, verbs []string) string {
	for _, v := range verbs {
		if strings.HasPrefix(s[k:], v) {
			return v
		}
	}
	return ""
}

var httpVerbs = []string{"get", "post", "put", "patch", "delete"}

// quotedAt reads [`'"]([^`'"]+)[`'"] at s[k]: the content and the index after it.
func quotedAt(s string, k int) (string, int, bool) {
	if k >= len(s) || !isQuote3(s[k]) {
		return "", 0, false
	}
	st := k + 1
	e := st
	for e < len(s) && !isQuote3(s[e]) {
		e++
	}
	if e == st || e >= len(s) {
		return "", 0, false
	}
	return s[st:e], e + 1, true
}

// callMatch is line.match(CALL_RE):
// /\b(?:api|http|httpClient|axios|client|fetcher)\s*\.\s*(get|post|put|patch|delete)\s*\(\s*[`'"]([^`'"]+)[`'"]/.
// Every candidate hangs off a dot, and their starts increase with the dot, so the
// first dot that completes is the leftmost match.
func callMatch(line string) (verb, path string, ok bool) {
	for from := 0; ; {
		j := strings.IndexByte(line[from:], '.')
		if j < 0 {
			return "", "", false
		}
		d := from + j
		from = d + 1
		k := skipSpace(line, d+1)
		v := verbAt(line, k, httpVerbs)
		if v == "" {
			continue
		}
		k = skipSpace(line, k+len(v))
		if k >= len(line) || line[k] != '(' {
			continue
		}
		e := skipSpaceBack(line, d)
		s := e
		for s > 0 && isWord(line[s-1]) {
			s--
		}
		if !callClients[line[s:e]] {
			continue
		}
		p, _, ok := quotedAt(line, skipSpace(line, k+1))
		if !ok {
			continue
		}
		return v, p, true
	}
}

// fetchMatch is line.match(FETCH_RE):
// /\bfetch\s*\(\s*[`'"]([^`'"]+)[`'"]\s*(?:,\s*\{[^}]*method\s*:\s*['"](\w+)['"])?/.
// method is "" when the optional group did not match.
func fetchMatch(line string) (path, method string, ok bool) {
	for from := 0; ; {
		j := strings.Index(line[from:], "fetch")
		if j < 0 {
			return "", "", false
		}
		pos := from + j
		from = pos + 1
		if !boundaryBefore(line, pos) {
			continue
		}
		k := skipSpace(line, pos+5)
		if k >= len(line) || line[k] != '(' {
			continue
		}
		p, end, ok := quotedAt(line, skipSpace(line, k+1))
		if !ok {
			continue
		}
		return p, fetchMethod(line, end), true
	}
}

// fetchMethod is the optional `\s*,\s*\{[^}]*method\s*:\s*['"](\w+)['"]` at end:
// the greedy [^}]* backtracks, so the last "method" before the first `}` wins.
func fetchMethod(line string, end int) string {
	r := skipSpace(line, end)
	if r >= len(line) || line[r] != ',' {
		return ""
	}
	r = skipSpace(line, r+1)
	if r >= len(line) || line[r] != '{' {
		return ""
	}
	window := line[r+1:]
	if b := strings.IndexByte(window, '}'); b >= 0 {
		window = window[:b]
	}
	for {
		j := strings.LastIndex(window, "method")
		if j < 0 {
			return ""
		}
		k := skipSpace(line, r+1+j+6)
		if k < len(line) && line[k] == ':' {
			k = skipSpace(line, k+1)
			if k < len(line) && isQuote2(line[k]) {
				s := k + 1
				e := s
				for e < len(line) && isWord(line[e]) {
					e++
				}
				if e > s && e < len(line) && isQuote2(line[e]) {
					return line[s:e]
				}
			}
		}
		window = window[:j]
	}
}

var trpcHooks = []string{"useQuery", "useMutation", "useInfiniteQuery", "useSuspenseQuery", "useQueries", "query", "mutate"}

func isProcChar(c byte) bool { return isWord(c) || c == '$' || c == '.' }

// trpcMatch is line.match(TRPC_CALL):
// /\btrpc(?:Client|Proxy)?\s*\.\s*([\w$.]+?)\s*\.\s*(useQuery|…|mutate)\s*\(/.
func trpcMatch(line string) (proc, hook string, ok bool) {
	for from := 0; ; {
		j := strings.Index(line[from:], "trpc")
		if j < 0 {
			return "", "", false
		}
		pos := from + j
		from = pos + 1
		if !boundaryBefore(line, pos) {
			continue
		}
		k := pos + 4
		if strings.HasPrefix(line[k:], "Client") {
			k += 6
		} else if strings.HasPrefix(line[k:], "Proxy") {
			k += 5
		}
		k = skipSpace(line, k)
		if k >= len(line) || line[k] != '.' {
			continue
		}
		g := skipSpace(line, k+1)
		max := g
		for max < len(line) && isProcChar(line[max]) {
			max++
		}
		// The lazy group grows one character at a time until the tail matches.
		for q := g + 1; q <= max; q++ {
			if h, ok := trpcTail(line, q); ok {
				return line[g:q], h, true
			}
		}
	}
}

func trpcTail(line string, q int) (string, bool) {
	q = skipSpace(line, q)
	if q >= len(line) || line[q] != '.' {
		return "", false
	}
	q = skipSpace(line, q+1)
	for _, h := range trpcHooks {
		if !strings.HasPrefix(line[q:], h) {
			continue
		}
		r := skipSpace(line, q+len(h))
		if r < len(line) && line[r] == '(' {
			return h, true
		}
	}
	return "", false
}

// reqIDs is text.match(REQ_ANY) (any=true) or the matches of
// /\bREQ-[A-Z0-9]+(?:-[A-Z0-9]+)*\b/g (any=false). REQ_ANY is
// /\b(?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b/g.
func reqIDs(t string, any bool) []string {
	var out []string
	from := 0
	for at := 0; at < len(t); {
		j := strings.IndexByte(t[at:], '-')
		if j < 0 {
			break
		}
		d := at + j
		at = d + 1
		s := -1
		switch {
		case d >= 3 && t[d-3:d] == "REQ":
			s = d - 3
		case any && d >= 3 && t[d-3:d] == "NFR" && d-3 >= from && boundaryBefore(t, d-3):
			s = d - 3
		case any && d >= 2 && t[d-2:d] == "FR":
			s = d - 2
		}
		if s < from || !boundaryBefore(t, s) {
			continue
		}
		if end, ok := reqTail(t, d+1); ok {
			out = append(out, t[s:end])
			from = end
			if end > at {
				at = end
			}
		}
	}
	return out
}

// reqTail matches [A-Z0-9]+(?:-[A-Z0-9]+)*\b from k, backtracking the way the
// regex does: when a word character follows the greedy span, the match ends
// before its last dash, or fails.
func reqTail(t string, k int) (int, bool) {
	e := k
	for e < len(t) && isUpperDigit(t[e]) {
		e++
	}
	if e == k {
		return 0, false
	}
	lastDash := -1
	for e+1 < len(t) && t[e] == '-' && isUpperDigit(t[e+1]) {
		lastDash = e
		e++
		for e < len(t) && isUpperDigit(t[e]) {
			e++
		}
	}
	if e == len(t) || !isWord(t[e]) {
		return e, true
	}
	if lastDash >= 0 {
		return lastDash, true
	}
	return 0, false
}

// prismaUses is every capture of /\b(?:prisma|tx|db)\.(\w+)\./g.
func prismaUses(t string) []string {
	var out []string
	from := 0
	for at := 0; at < len(t); {
		j := strings.IndexByte(t[at:], '.')
		if j < 0 {
			break
		}
		d := at + j
		at = d + 1
		s := -1
		switch {
		case d >= 6 && t[d-6:d] == "prisma":
			s = d - 6
		case d >= 2 && (t[d-2:d] == "tx" || t[d-2:d] == "db"):
			s = d - 2
		}
		if s < from || !boundaryBefore(t, s) {
			continue
		}
		e := d + 1
		for e < len(t) && isWord(t[e]) {
			e++
		}
		if e == d+1 || e >= len(t) || t[e] != '.' {
			continue
		}
		out = append(out, t[d+1:e])
		from = e + 1
		at = e + 1
	}
	return out
}

// callish is CALLISH.test(text):
// /\bfetch\s*\(|\baxios\s*\.\s*(get|post|put|patch|delete|request)\s*\(|\buse(Query|Mutation|SWR|InfiniteQuery)\s*\(|\$fetch\s*\(|\btrpc\s*\.|\bky\s*\(|\bgot\s*\(/.
// `$fetch(` is covered by `\bfetch(`: `$` is not a word character.
func callish(t string) bool {
	each := func(word string, rest func(k int) bool) bool {
		for from := 0; ; {
			j := strings.Index(t[from:], word)
			if j < 0 {
				return false
			}
			pos := from + j
			from = pos + 1
			if boundaryBefore(t, pos) && rest(pos+len(word)) {
				return true
			}
		}
	}
	paren := func(k int) bool {
		k = skipSpace(t, k)
		return k < len(t) && t[k] == '('
	}
	dot := func(k int) bool {
		k = skipSpace(t, k)
		return k < len(t) && t[k] == '.'
	}
	axiosVerbs := []string{"get", "post", "put", "patch", "delete", "request"}
	hooks := []string{"Query", "Mutation", "SWR", "InfiniteQuery"}
	return each("fetch", paren) ||
		each("axios", func(k int) bool {
			k = skipSpace(t, k)
			if k >= len(t) || t[k] != '.' {
				return false
			}
			k = skipSpace(t, k+1)
			v := verbAt(t, k, axiosVerbs)
			return v != "" && paren(k+len(v))
		}) ||
		each("use", func(k int) bool {
			h := verbAt(t, k, hooks)
			return h != "" && paren(k+len(h))
		}) ||
		each("trpc", dot) ||
		each("ky", paren) ||
		each("got", paren)
}

// hasProcedure is /\b(?:publicProcedure|protectedProcedure|procedure|t\.procedure)\b/.test(text).
// `t.procedure` is covered by `\bprocedure\b`.
func hasProcedure(t string) bool {
	after := func(e int) bool { return e >= len(t) || !isWord(t[e]) }
	for from := 0; ; {
		j := strings.Index(t[from:], "rocedure")
		if j < 0 {
			return false
		}
		r := from + j // index of "rocedure"
		from = r + 1
		if !after(r + 8) {
			continue
		}
		switch {
		case r >= 1 && t[r-1] == 'p' && boundaryBefore(t, r-1):
			return true
		case r >= 7 && t[r-7:r] == "publicP" && boundaryBefore(t, r-7):
			return true
		case r >= 10 && t[r-10:r] == "protectedP" && boundaryBefore(t, r-10):
			return true
		}
	}
}

// hasRouterCall is /\brouter\s*\(\s*\{/.test(text).
func hasRouterCall(t string) bool {
	for from := 0; ; {
		j := strings.Index(t[from:], "router")
		if j < 0 {
			return false
		}
		pos := from + j
		from = pos + 1
		if !boundaryBefore(t, pos) {
			continue
		}
		k := skipSpace(t, pos+6)
		if k >= len(t) || t[k] != '(' {
			continue
		}
		k = skipSpace(t, k+1)
		if k < len(t) && t[k] == '{' {
			return true
		}
	}
}
