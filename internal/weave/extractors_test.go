package weave

import (
	"sort"
	"strings"
	"testing"
)

// A router split across files: the root aliases a sub-router, which nests a plain
// object and names procedures defined in their own modules.
func TestTRPCAcrossFiles(t *testing.T) {
	files := []SourceFile{
		src("server/router.ts", "import { teamRouter } from './team/router';\nimport { router } from './trpc';\nexport const appRouter = router({\n  team: teamRouter,\n  ping: publicProcedure.query(() => 'pong'),\n});\n"),
		src("server/team/router.ts", "import { router, authenticatedProcedure } from '../trpc';\nimport { findTeamsRoute } from './find-teams';\nexport const teamRouter = router({\n  find: findTeamsRoute,\n  member: {\n    remove: authenticatedProcedure.input(z).mutation(() => 1),\n  },\n});\n"),
		src("server/team/find-teams.ts", "import { authenticatedProcedure } from '../trpc';\nexport const findTeamsRoute = authenticatedProcedure\n  .input(Z)\n  .query(async () => []);\n"),
	}
	var got []string
	for _, p := range ResolveTRPC(files) {
		got = append(got, p.kind+" "+p.name+" "+p.file)
	}
	sort.Strings(got)
	want := []string{
		"MUTATION team.member.remove server/team/router.ts",
		"QUERY ping server/router.ts",
		"QUERY team.find server/team/find-teams.ts",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRemixRoutes(t *testing.T) {
	known := map[string]bool{"apps/web/app/root.tsx": true}
	for rel, want := range map[string]string{
		"apps/web/app/routes/_index.tsx":                              "/",
		"apps/web/app/routes/api.render.tsx":                          "/api/render",
		"apps/web/app/routes/api+/preferred-team.tsx":                 "/api/preferred-team",
		"apps/web/app/routes/_auth+/t.$teamUrl+/documents._index.tsx": "/t/:teamUrl/documents",
		"apps/web/app/routes/blog.$slug/route.tsx":                    "/blog/:slug",
		"apps/web/app/routes/files.$.tsx":                             "/files/*",
		"apps/web/app/routes/blog.$slug/helpers.ts":                   "",
		"apps/web/app/routes/api+/+utils.ts":                          "",
		"other/app/routes/x.tsx":                                      "", // no root module beside it
	} {
		got, ok := remixRoute(rel, known)
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", rel, got, want)
		}
	}
}

func TestExternalCallsAreNotContracts(t *testing.T) {
	calls := ExtractCalls([]SourceFile{src("a.ts", "fetch('https://stats.example.com/api/stats')\nfetch('http://localhost:3000/api/x')\naxios.get('https://api.github.com/repos')\n")})
	if len(calls) != 1 || !strings.Contains(calls[0].Path, "localhost") {
		t.Fatalf("calls: %v", calls)
	}
}

func TestClientLikeReceivers(t *testing.T) {
	calls := ExtractCalls([]SourceFile{src("a.js", "requests.get('/articles')\napiClient.post('/users', body)\nmap.get('/x')\nusersRouter.get('/me', h)\nrequests.get(key)\n")})
	var got []string
	for _, c := range calls {
		got = append(got, c.Method+" "+c.Path)
	}
	if strings.Join(got, ",") != "GET /articles,POST /users" {
		t.Fatalf("calls: %v", got)
	}
}

func TestRouteAttributesInAnyOrder(t *testing.T) {
	b := NewBuilder()
	files := []SourceFile{
		src("src/components/App.js", "import Home from './Home';\nimport Article from './Article';\n<Route exact path=\"/\" element={<Home />} />\n<Route element={<Article />} path=\"/article/:slug\" />\n"),
		src("src/components/Home.js", "export default function Home() {}\n"),
		src("src/components/Article.js", "export default function Article() {}\n"),
	}
	info := ExtractPages(files, b, files)
	if info.Declared != 2 || len(info.Pages) != 2 || info.Pages[1].Route != "/article/:slug" || info.Pages[1].Component != "Article" {
		t.Fatalf("pages: %+v", info)
	}
}
