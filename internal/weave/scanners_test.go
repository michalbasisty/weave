package weave

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// The hand scanners must find exactly what the JavaScript regexes find. RE2 is a
// faithful oracle here: these patterns have no lookarounds or backreferences, so
// leftmost-first matching gives V8's matches. RE2 is only too slow to ship.

var (
	oracleCall     = jsRe("\\b(?:api|http|httpClient|axios|client|fetcher)\\s*\\.\\s*(get|post|put|patch|delete)\\s*\\(\\s*[`'\"]([^`'\"]+)[`'\"]")
	oracleFetch    = jsRe("\\bfetch\\s*\\(\\s*[`'\"]([^`'\"]+)[`'\"]\\s*(?:,\\s*\\{[^}]*method\\s*:\\s*['\"](\\w+)['\"])?")
	oracleTRPC     = jsRe(`\btrpc(?:Client|Proxy)?\s*\.\s*([\w$.]+?)\s*\.\s*(useQuery|useMutation|useInfiniteQuery|useSuspenseQuery|useQueries|query|mutate)\s*\(`)
	oracleReqAny   = jsRe(`\b(?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`)
	oracleReqOnly  = jsRe(`\bREQ-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`)
	oracleImport   = jsRe(`(?:^|\n)\s*(?:import|export)[^'"]*?from\s*['"]([^'"]+)['"]`)
	oraclePrisma   = jsRe(`\b(?:prisma|tx|db)\.(\w+)\.`)
	oracleCallish  = jsRe(`\bfetch\s*\(|\baxios\s*\.\s*(get|post|put|patch|delete|request)\s*\(|\buse(Query|Mutation|SWR|InfiniteQuery)\s*\(|\$fetch\s*\(|\btrpc\s*\.|\bky\s*\(|\bgot\s*\(`)
	oracleProc     = jsRe(`\b(?:publicProcedure|protectedProcedure|procedure|t\.procedure)\b`)
	oracleRouterFn = jsRe(`\brouter\s*\(\s*\{`)
)

var tokens = []string{
	"api", "http", "httpClient", "axios", "client", "fetcher", "fetch", "$fetch", ".", ". ", " ", "  ", "\t", " ", " ", "\r",
	"get", "post", "put", "patch", "delete", "request", "(", ")", "'", "\"", "`", "/api/x", "/orders", "${id}", "?q=1",
	",", "{", "}", "method", ":", "POST", "'PUT'", "trpc", "Client", "Proxy", "useQuery", "useMutation", "useQueries",
	"useInfiniteQuery", "useSWR", "query", "mutate", "x", "$", "_", "a.b", "REQ", "FR", "NFR", "-", "A", "1", "b", "Z9",
	"import", "export", "from", "prisma", "tx", "db", "use", "SWR", "ky", "got", "router", "procedure", "publicProcedure",
	"protectedProcedure", "t.", "é", "\n",
}

func randomText(r *rand.Rand, lines bool) string {
	var b strings.Builder
	for n := 1 + r.Intn(14); n > 0; n-- {
		tok := tokens[r.Intn(len(tokens))]
		if lines && tok == "\n" {
			continue
		}
		b.WriteString(tok)
	}
	return b.String()
}

func sub(m []string, i int) string {
	if m == nil {
		return ""
	}
	return m[i]
}

func TestScannersMatchRegexes(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 300000; i++ {
		line := randomText(r, true)
		text := randomText(r, false) + randomText(r, false)

		v, p, ok := callMatch(line)
		m := oracleCall.FindStringSubmatch(line)
		if ok != (m != nil) || v != sub(m, 1) || p != sub(m, 2) {
			t.Fatalf("CALL_RE %q: scanner (%q %q %v), regex %q", line, v, p, ok, m)
		}
		p, meth, ok := fetchMatch(line)
		m = oracleFetch.FindStringSubmatch(line)
		if ok != (m != nil) || p != sub(m, 1) || meth != sub(m, 2) {
			t.Fatalf("FETCH_RE %q: scanner (%q %q %v), regex %q", line, p, meth, ok, m)
		}
		proc, hook, ok := trpcMatch(line)
		m = oracleTRPC.FindStringSubmatch(line)
		if ok != (m != nil) || proc != sub(m, 1) || hook != sub(m, 2) {
			t.Fatalf("TRPC_CALL %q: scanner (%q %q %v), regex %q", line, proc, hook, ok, m)
		}
		if got, want := reqIDs(text, true), oracleReqAny.FindAllString(text, -1); !sameStrings(got, want) {
			t.Fatalf("REQ_ANY %q: scanner %q, regex %q", text, got, want)
		}
		if got, want := reqIDs(text, false), oracleReqOnly.FindAllString(text, -1); !sameStrings(got, want) {
			t.Fatalf("REQ- %q: scanner %q, regex %q", text, got, want)
		}
		if got, want := importSpecs(text), captures(oracleImport, text); !sameStrings(got, want) {
			t.Fatalf("IMPORT_RE %q: scanner %q, regex %q", text, got, want)
		}
		if got, want := prismaUses(text), captures(oraclePrisma, text); !sameStrings(got, want) {
			t.Fatalf("prisma uses %q: scanner %q, regex %q", text, got, want)
		}
		if got, want := callish(text), oracleCallish.MatchString(text); got != want {
			t.Fatalf("CALLISH %q: scanner %v, regex %v", text, got, want)
		}
		if got, want := hasProcedure(text), oracleProc.MatchString(text); got != want {
			t.Fatalf("procedure %q: scanner %v, regex %v", text, got, want)
		}
		if got, want := hasRouterCall(text), oracleRouterFn.MatchString(text); got != want {
			t.Fatalf("router({ %q: scanner %v, regex %v", text, got, want)
		}
	}
}

func captures(re interface {
	FindAllStringSubmatch(string, int) [][]string
}, s string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// Hand-picked shapes the random tokens rarely build.
func TestScannerCases(t *testing.T) {
	cases := []struct {
		got, want any
	}{
		{reqIDs("REQ-A-b REQ-AB_c NFR-1 xFR-2 (FR-3-X) NFREQ-4", true), []string{"REQ-A", "NFR-1", "FR-3-X"}},
		{importSpecs("import a from 'x'\nexport { b } from \"./y\"\n  import c,\n  { d } from './z'"), []string{"x", "./y", "./z"}},
		{prismaUses("await prisma.order.find(); tx.user.update; $db.a.b; xdb.c.d"), []string{"order", "user", "a"}},
	}
	for i, c := range cases {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
	if v, p, ok := callMatch(`  const r = await api . post ( "/orders", body)`); !ok || v != "post" || p != "/orders" {
		t.Errorf("callMatch: %q %q %v", v, p, ok)
	}
	if p, m, ok := fetchMatch("fetch(`/api/a`, { method: 'GET', x: 1, method: 'DELETE' })"); !ok || p != "/api/a" || m != "DELETE" {
		t.Errorf("fetchMatch: %q %q %v", p, m, ok)
	}
	if proc, hook, ok := trpcMatch("trpc.quotes.list.useQuery()"); !ok || proc != "quotes.list" || hook != "useQuery" {
		t.Errorf("trpcMatch: %q %q %v", proc, hook, ok)
	}
}
