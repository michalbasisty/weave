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
	for _, p := range ResolveTRPC(files, nil) {
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
	info := ExtractPages(files, b, files, nil)
	if info.Declared != 2 || len(info.Pages) != 2 || info.Pages[1].Route != "/article/:slug" || info.Pages[1].Component != "Article" {
		t.Fatalf("pages: %+v", info)
	}
}

func edgeSet(b *Builder) map[string]bool {
	out := map[string]bool{}
	for _, e := range b.edges {
		out[e.From+" -"+e.Kind+"-> "+e.To] = true
	}
	return out
}

// Monorepo imports name packages and tsconfig paths, not relative files.
func TestAliasImports(t *testing.T) {
	files := []SourceFile{
		src("packages/lib/package.json", `{"name": "@acme/lib"}`),
		src("packages/lib/server/teams.ts", "export const x = 1\n"),
		src("packages/ui/package.json", `{"name": "@acme/ui"}`),
		src("packages/ui/src/button.tsx", "export const Button = 1\n"),
		src("apps/web/tsconfig.json", "{\n  // comment\n  \"compilerOptions\": { \"baseUrl\": \".\", \"paths\": { \"~/*\": [\"./app/*\"], }, },\n}\n"),
		src("apps/web/app/utils/x.types.ts", "export type X = 1\n"),
		src("apps/web/app/routes/a.tsx", "\n"),
	}
	al := LoadAliases(files)
	known := knownSet(files)
	for spec, want := range map[string]string{
		"@acme/lib/server/teams": "packages/lib/server/teams.ts",
		"@acme/ui/button":        "packages/ui/src/button.tsx",
		"~/utils/x.types":        "apps/web/app/utils/x.types.ts",
		"./a":                    "apps/web/app/routes/a.tsx",
		"react":                  "",
	} {
		if got := al.Resolve("apps/web/app/routes/a.tsx", spec, known); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", spec, got, want)
		}
	}
}

func TestDrizzleAndTypeORM(t *testing.T) {
	files := []SourceFile{
		src("src/db/schema.ts", "export const users = pgTable('users', { id: serial('id') })\nexport const orders = mysqlTable(\"orders\", {})\n"),
		src("src/entities/line-item.ts", "@Entity()\nexport class LineItem {}\n@Entity({ name: 'customers' })\nexport class Customer {}\n"),
		src("src/orders/service.ts", "db.select().from(orders).innerJoin(users, eq(a, b))\n"),
		src("src/billing/service.ts", "constructor(@InjectRepository(Customer) private r: Repository<Customer>) {}\nthis.repo.find(LineItem)\n"),
	}
	b := NewBuilder()
	var got []string
	for _, m := range ExtractORMModels(files, b) {
		got = append(got, m.Name+"="+m.Table)
	}
	sort.Strings(got)
	if want := "Customer=customers LineItem=line_item orders=orders users=users"; strings.Join(got, " ") != want {
		t.Fatalf("models %v, want %s", got, want)
	}
	edges := edgeSet(b)
	for _, want := range []string{
		"module:orders -uses-> model:orders",
		"module:orders -uses-> model:users",
		"module:billing -uses-> model:Customer",
		"module:billing -uses-> model:LineItem",
	} {
		if !edges[want] {
			t.Errorf("missing edge %s", want)
		}
	}
}

func TestNestAndFastifyRoutes(t *testing.T) {
	files := []SourceFile{
		src("src/main.ts", "app.setGlobalPrefix('api')\n"),
		src("src/users/users.controller.ts", "@Controller('users')\nexport class UsersController {\n  @Get()\n  list() {}\n  @Get(':id')\n  one() {}\n  @Post()\n  create() {}\n}\n@Controller({ path: 'admin' })\nexport class AdminController {\n  @All('*')\n  any() {}\n}\n"),
		src("src/orders/routes.ts", "fastify.route({ method: 'POST', url: '/orders', handler })\nfastify.route({ url: '/orders/:id', method: ['GET'], handler })\n"),
	}
	b := NewBuilder()
	var got []string
	for _, e := range ExtractNestAndFastify(files, b) {
		got = append(got, e.Method+" "+e.Path)
	}
	sort.Strings(got)
	want := []string{"ANY /api/admin/*", "GET /api/users", "GET /api/users/:id", "GET /orders/:id", "POST /api/users", "POST /orders"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
