package weave

import (
	"strings"
	"testing"
)

// Each case is a false alarm weave raised on a real app's clean tree (Papermark,
// Linkwarden, Outline): a call the repo serves, reported as broken.

// `${team?.id}` is JavaScript, not a query string: the path keeps its segments.
func TestOptionalChainInTemplateIsNotAQuery(t *testing.T) {
	for in, want := range map[string]string{
		"/api/teams/${teamInfo?.currentTeam?.id}":         "/api/teams/${teamInfo?.currentTeam?.id}",
		"/api/unsubscribe/${type}?token=${token}":         "/api/unsubscribe/${type}",
		"/api/x/${a ? b : c}/y?z=1":                       "/api/x/${a ? b : c}/y",
		"/api/teams/${t?.id}/documents/${d}?from=${x?.y}": "/api/teams/${t?.id}/documents/${d}",
	} {
		if got := WithoutQuery(in); got != want {
			t.Errorf("WithoutQuery(%q) = %q, want %q", in, got, want)
		}
	}
	eps := []Endpoint{{Method: "DELETE", Path: "/api/teams/:teamId"}}
	if MatchEndpoint(Call{Method: "DELETE", Path: "/api/teams/${teamInfo?.currentTeam?.id}"}, eps) == nil {
		t.Error("DELETE /api/teams/${teamInfo?.currentTeam?.id} did not reach /api/teams/:teamId")
	}
}

// `"/api/collections/" + id` is `/api/collections/${id}`, and a method set two
// braces into the options (`body: JSON.stringify({…})`) is still read.
func TestConcatenatedPathAndNestedOptions(t *testing.T) {
	calls := ExtractCalls([]SourceFile{
		src("a.ts", `const res = await fetch("/api/v1/public/collections/" + collectionId);`),
		src("b.ts", "await fetch(\"/api/v1/worker/preservation\", {\n  body: JSON.stringify({\n    action: payload.action,\n  }),\n  method: \"DELETE\",\n});\n"),
		src("c.ts", `api.get("/api/users/" + user.id + "/links")`),
		src("d.ts", `fetch("/api/search?q=" + q)`),
	})
	got := map[string]string{}
	for _, c := range calls {
		got[c.File] = c.Method + " " + c.Path
	}
	for file, want := range map[string]string{
		"a.ts": "GET /api/v1/public/collections/${collectionId}",
		"b.ts": "DELETE /api/v1/worker/preservation",
		"c.ts": "GET /api/users/${user.id}/links",
		"d.ts": "GET /api/search?q=",
	} {
		if got[file] != want {
			t.Errorf("%s: got %q, want %q", file, got[file], want)
		}
	}
}

// Next API routes: `[[...x]]` serves the bare path too, `[...x]` one segment or
// more, and `req.method !== "POST"` or `switch (req.method)` name the methods.
func TestNextCatchAllsAndMethodChecks(t *testing.T) {
	b := NewBuilder()
	st := ExtractNextRoutes([]SourceFile{
		src("pages/api/conversations/[[...conversations]].ts", "export default async function h(req, res) {}\n"),
		src("pages/api/folders/[...name].ts", "export default async function h(req, res) { if (req.method === \"GET\") {} }\n"),
		src("pages/api/unsubscribe/yir/index.ts", "export default async function h(req, res) {\n  if (req.method !== \"POST\" && req.method !== \"GET\") return\n}\n"),
		src("pages/api/links/[id]/index.ts", "export default async function h(req, res) {\n  switch (req.method) {\n    case \"PUT\":\n    case 'DELETE':\n  }\n}\n"),
	}, b)
	var names []string
	for _, e := range st.Endpoints {
		names = append(names, e.Method+" "+e.Path)
	}
	all := strings.Join(names, ", ")
	for _, want := range []string{"ANY /api/conversations/**", "GET /api/folders/*", "POST /api/unsubscribe/yir", "GET /api/unsubscribe/yir", "PUT /api/links/:id", "DELETE /api/links/:id"} {
		if !strings.Contains(", "+all+",", ", "+want+",") {
			t.Errorf("missing %s in %s", want, all)
		}
	}
	for call, want := range map[string]string{
		"POST /api/conversations":                  "/api/conversations/**",
		"POST /api/conversations/${id}/messages":   "/api/conversations/**",
		"GET /api/folders/${a}/${b}":               "/api/folders/*",
		"GET /api/folders":                         "",
		"POST /api/unsubscribe/yir?token=${token}": "/api/unsubscribe/yir",
		"DELETE /api/links/${link?.id}":            "/api/links/:id",
	} {
		m, p, _ := strings.Cut(call, " ")
		got := ""
		if e := MatchEndpoint(Call{Method: m, Path: p}, st.Endpoints); e != nil {
			got = e.Path
		}
		if got != want {
			t.Errorf("%s matched %q, want %q", call, got, want)
		}
	}
}

// @koa/router takes paths without a slash (`router.post("users.list", …)`), and a
// test that drives the server (`server.post("/api/users.info")`) declares nothing.
func TestKoaRouterAndTestFiles(t *testing.T) {
	b := NewBuilder()
	eps := ExtractEndpoints([]SourceFile{
		src("server/routes/api/users.ts", "import Router from \"@koa/router\";\nconst router = new Router();\nrouter.post(\n  \"users.list\",\n  auth(),\n  async (ctx) => {}\n);\nrouter.post(\"users.info\", auth(), h);\nexport default router;\n"),
		src("server/routes/api/users.test.ts", "const server = getTestServer();\nconst res = await server.post(\"/api/users.ghost\", { body: {} });\n"),
	}, b)
	var names []string
	for _, e := range eps {
		names = append(names, e.Method+" "+e.Path)
	}
	all := strings.Join(names, ", ")
	if !strings.Contains(all, "POST /users.list") || !strings.Contains(all, "POST /users.info") {
		t.Errorf("koa routes: %s", all)
	}
	if strings.Contains(all, "users.ghost") {
		t.Errorf("a test's request became an endpoint: %s", all)
	}
	if MatchEndpoint(Call{Method: "POST", Path: "/users.list"}, eps) == nil {
		t.Error("client.post(`/users.list`) did not reach the koa route")
	}
}
