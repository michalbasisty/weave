package grep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

func fixtureGraph(t *testing.T) *weave.Graph {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "test", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	g := weave.BuildGraph(root)
	g.Root = root
	return g
}

func TestGrepMatchesTheTypeScript(t *testing.T) {
	g := fixtureGraph(t)
	want := `grep: 16 line(s) for "orders"
  packages/backend/prisma/schema.prisma:6: @@map("orders")
  packages/backend/src/app.ts:2: import orderRoutes from './modules/orders/orders.routes.js';
  packages/backend/src/app.ts:5: app.use('/api/v1/orders', orderRoutes);
  packages/web/e2e/orders.spec.ts:5: test('orders page loads', async ({ page }) => {
  packages/web/e2e/orders.spec.ts:6: await page.goto(BASE + '/orders');
  packages/web/src/App.tsx:4: const Orders = lazy(() => import('./pages/Orders'));
  packages/web/src/App.tsx:9: <Route path="/orders" element={<Orders />} />
  packages/web/src/pages/Orders.tsx:4: export default function Orders() {
  packages/web/src/pages/Orders.tsx:8: api.get('/orders').then((r) => setRows(r.data));
  specs/changes/2026-01-01-orders/spec-delta.md:1: # Delta: orders
  specs/changes/2026-01-01-orders/spec-delta.md:3: ### ADDED REQ-ORD-1 — list orders
  specs/changes/2026-01-01-orders/spec-delta.md:5: WHEN a user opens the warehouse, THE system SHALL list orders newest first.
  specs/changes/2026-01-01-orders/tasks.md:1: # Tasks: Orders (2026-01-01)
  specs/changes/2026-01-01-orders/tasks.md:3: - [x] T1 API ` + "`" + `/orders` + "`" + ` list + create.
  specs/changes/2026-01-01-orders/tasks.md:4: Evidence: ` + "`" + `modules/orders/orders.routes.ts` + "`" + `, ` + "`" + `prisma/schema.prisma` + "`" + `.
  specs/changes/2026-01-01-orders/tasks.md:6: Evidence: ` + "`" + `pages/Orders.tsx` + "`" + `.`
	if got := Grep(g, "orders", 20, false); got != want {
		t.Errorf("grep orders:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestGrepLimitAndRegex(t *testing.T) {
	g := fixtureGraph(t)
	want := `grep: 13 (+11 more) line(s) for "import"
  packages/backend/src/app.ts:1: import express from 'express';
  packages/backend/src/app.ts:2: import orderRoutes from './modules/orders/orders.routes.js';`
	if got := Grep(g, "import", 2, false); got != want {
		t.Errorf("grep import --limit 2:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	want = `grep: 1 line(s) for "app\.(get|post|use)"
  packages/backend/src/app.ts:5: app.use('/api/v1/orders', orderRoutes);`
	if got := Grep(g, `app\.(get|post|use)`, 20, true); got != want {
		t.Errorf("grep --regex:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestGrepEmptyAndBadRegex(t *testing.T) {
	g := fixtureGraph(t)
	if got, want := Grep(g, "  ", 20, false), "grep needs a query"; got != want {
		t.Errorf("empty query = %q, want %q", got, want)
	}
	if got, want := Grep(g, "GET /", 20, true), `grep: no match for "GET /"`; got != want {
		t.Errorf("no match = %q, want %q", got, want)
	}
	if got, want := Grep(g, "zzzz", 20, false), `grep: no match for "zzzz"`; got != want {
		t.Errorf("no match = %q, want %q", got, want)
	}
	want := "bad regex: Invalid regular expression: /[/i: Unterminated character class"
	if got := Grep(g, "[", 20, true); got != want {
		t.Errorf("bad regex = %q, want %q", got, want)
	}
}

func TestGrepSkipsLongLinesAndCapsHits(t *testing.T) {
	dir := t.TempDir()
	long := "hit " + strings.Repeat("a", 396)   // 400 UTF-16 units: kept
	longer := "hit " + strings.Repeat("a", 397) // 401: skipped
	if err := os.WriteFile(filepath.Join(dir, "short.ts"), []byte(long+"\n"+longer+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &weave.Graph{Root: dir}
	if got, want := Grep(g, "hit", 20, false), "grep: 1 line(s) for \"hit\"\n  short.ts:1: "+long[:160]; got != want {
		t.Errorf("long-line filter:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	var b strings.Builder
	for i := 0; i < 5002; i++ {
		fmt.Fprintf(&b, "hit %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "many.ts"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Grep(g, "hit", 1, false)
	want := "grep: 5002 (+5001 more) line(s) for \"hit\"\n  many.ts:1: hit 0"
	if got != want {
		t.Errorf("hit cap:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// Source comes before prose, and archived prose comes last.
func TestSourceOutranksArchivedDocs(t *testing.T) {
	order := []string{"packages/api/tasks.ts", "packages/api/tasks.json", "docs/plan.md", "docs/archive/19-offline-plan.md"}
	for i := 1; i < len(order); i++ {
		if rankOf(order[i-1], false) >= rankOf(order[i], false) {
			t.Fatalf("%s should rank above %s", order[i-1], order[i])
		}
	}
	if rankOf("src/app.ts", true) >= rankOf("src/app.ts", false) {
		t.Fatal("a file on the map ranks first")
	}
}
