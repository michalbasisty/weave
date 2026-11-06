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

func TestGraphQL(t *testing.T) {
	files := []SourceFile{
		src("api/schema.graphql", "\"\"\"Root\"\"\"\ntype Query {\n  users(first: Int = 10): [User!]!\n  # a comment: not a field\n  user(id: ID!): User\n}\nextend type Mutation { createUser(input: UserInput!): User! }\ntype User { id: ID! name: String }\n"),
		src("api/orders.resolver.ts", "@Resolver()\nexport class OrdersResolver {\n  @Query(() => [Order], { name: 'orders' })\n  async list() {}\n  @Mutation(() => Order)\n  cancelOrder(@Args('id') id: string) {}\n}\n"),
		src("api/pothos.ts", "builder.queryField('me', (t) => t.field({}))\n"),
		src("web/users.tsx", "const Q = gql`\n  query Users($n: Int) {\n    list: users(first: $n) { id ...UserBits }\n    me { id }\n  }\n`\nconst M = gql`mutation { createUser(input: $i) { id } }`\nconst B = gql`query { missing { id } }`\n"),
	}
	b := NewBuilder()
	eps, calls := ExtractGraphQL(files, b)
	var got []string
	for _, e := range eps {
		got = append(got, e.Method+" "+e.Path)
	}
	sort.Strings(got)
	want := []string{"MUTATION /graphql/cancelOrder", "MUTATION /graphql/createUser", "QUERY /graphql/me", "QUERY /graphql/orders", "QUERY /graphql/user", "QUERY /graphql/users"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("endpoints\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	got = nil
	for _, c := range calls {
		got = append(got, c.Method+" "+c.Path)
	}
	sort.Strings(got)
	want = []string{"MUTATION /graphql/createUser", "QUERY /graphql/me", "QUERY /graphql/missing", "QUERY /graphql/users"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// A front end with no schema of its own calls nothing it can break.
	if e, c := ExtractGraphQL(files[3:], NewBuilder()); len(e)+len(c) != 0 {
		t.Fatalf("client-only repo produced %v %v", e, c)
	}
}

// `lazy(() => import(/* webpackChunkName: "x" */ '../a'))` names its screen's file.
func TestLazyImportWithMagicComment(t *testing.T) {
	files := []SourceFile{
		src("src/App.js", "const Article = lazy(() =>\n  import(\n    /* webpackChunkName: \"Article\" */ './Article'\n  )\n);\nexport default () => <Routes><Route path=\"/article/:slug\" element={<Article />} /></Routes>\n"),
		src("src/Article.js", "export default function Article() {}\n"),
	}
	st := ExtractPages(files, NewBuilder(), files, nil)
	if len(st.Pages) != 1 || st.Pages[0].File != "src/Article.js" {
		t.Fatalf("pages = %+v", st.Pages)
	}
}

// Renaming a route must leave its callers unmatched, not quietly re-matched to a
// neighbour: a variable leaf is never a fixed route, and a longer call needs a
// literal in common with a shorter endpoint.
func TestCallsDoNotFallIntoNeighbourRoutes(t *testing.T) {
	eps := []Endpoint{
		{Method: "GET", Path: "/articles/feed"},
		{Method: "GET", Path: "/articles/:slug"},
		{Method: "GET", Path: "/api/users/:id"},
	}
	for path, want := range map[string]string{
		"/articles/${slug}":          "/articles/:slug",
		"/articles/feed":             "/articles/feed",
		"/articles/${slug}/comments": "",
		"/v1/api/users/${id}":        "/api/users/:id",
	} {
		got := ""
		if e := MatchEndpoint(Call{Method: "GET", Path: path}, eps); e != nil {
			got = e.Path
		}
		if got != want {
			t.Errorf("%s matched %q, want %q", path, got, want)
		}
	}
	// with /articles/:slug renamed away, its callers are broken, not "feed"
	if e := MatchEndpoint(Call{Method: "GET", Path: "/articles/${slug}"}, eps[:1]); e != nil {
		t.Errorf("/articles/${slug} matched %s", e.Path)
	}
}

// A template segment typed as a literal union is a choice between fixed routes, not a
// broken call; a value that matches no route is still caught.
func TestUnionSegmentIsAChoiceOfRoutes(t *testing.T) {
	eps := []Endpoint{
		{Method: "PUT", Path: "/tasks/:id/start", ID: "s"},
		{Method: "PUT", Path: "/tasks/:id/stop", ID: "t"},
		{Method: "GET", Path: "/articles/feed", ID: "f"},
		{Method: "GET", Path: "/articles/:slug/comments", ID: "c"},
	}
	text := "type TaskAction = 'start' | 'stop'\nconst act = (id: string, action: TaskAction) => api.put(`/tasks/${id}/${action}`)\n"
	c := Call{Method: "PUT", Path: "/tasks/${id}/${action}", File: "a.ts", Line: 2}
	got := expandUnion(c, text, unionAliases([]SourceFile{{Rel: "a.ts", Text: text}}))
	if len(got) != 2 || got[0] != "/tasks/${id}/start" || got[1] != "/tasks/${id}/stop" {
		t.Fatalf("expand: %v", got)
	}
	inline := "function f(id: string, action: 'start' | 'pause') {}"
	if got := expandUnion(c, inline, nil); len(got) != 2 || MatchEndpoint(Call{Method: "PUT", Path: got[1]}, eps) != nil {
		t.Fatalf("inline union should expand and 'pause' should not match: %v", got)
	}
	if got := expandUnion(c, "const x = { action: 'start' }", nil); got != nil {
		t.Fatalf("an object property is not a type: %v", got)
	}
	// type unknown: two fixed siblings are a choice, one is not
	if got := variableLeafChoices(c, eps); len(got) != 2 {
		t.Fatalf("choices: %v", got)
	}
	if got := variableLeafChoices(Call{Method: "GET", Path: "/articles/${slug}"}, eps); got != nil {
		t.Fatalf("/articles/${slug} must not become /articles/feed: %v", got)
	}
}
