package weave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A shared client that calls nothing itself still has a blast radius: the files
// that import it call 193 endpoints on QR-gent, and "impact" used to answer zero.
func TestImpactViaImportersCalls(t *testing.T) {
	root := copyFixture(t)
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("packages/web/src/services/client.ts", "export const api = { get: (p: string) => fetch(p) };\n")
	write("packages/web/src/services/users.ts", `import { api } from './client.js';

export const loadUsers = () => api.get('/api/v1/orders');
`)
	g := BuildGraph(root)

	imp := Impact(g, []string{"packages/web/src/services/client.ts"})
	if !strings.Contains(imp, "via") || !strings.Contains(imp, "GET /api/v1/orders") {
		t.Fatalf("impact did not follow the importer's call:\n%s", imp)
	}
	if strings.Contains(imp, "looks isolated") {
		t.Fatalf("impact called a widely imported file isolated:\n%s", imp)
	}

	ctx := Context(g, []string{"packages/web/src/services/client.ts"}, 900, "")
	if !strings.Contains(ctx, "via") {
		t.Fatalf("context did not warn about the importer's calls:\n%s", ctx)
	}
}

// A file that reaches nothing and is imported by nothing is still isolated.
func TestImpactIsolatedWhenNothingFollows(t *testing.T) {
	root := copyFixture(t)
	p := filepath.Join(root, "packages", "web", "src", "services", "alone.ts")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("export const alone = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := BuildGraph(root)
	out := Impact(g, []string{"packages/web/src/services/alone.ts"})
	if !strings.Contains(out, "looks isolated") {
		t.Fatalf("expected an isolated verdict:\n%s", out)
	}
}

// A `~` in context means an inferred page link; the legend must survive the budget.
func TestContextExplainsInferredMarker(t *testing.T) {
	root := copyFixture(t)
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("packages/web/src/services/ordersApi.ts", "export const load = () => api.get('/api/v1/orders');\n")
	write("packages/web/src/pages/Lazy.tsx", "import { load } from '../services/ordersApi';\n\nexport default function Lazy() { return load(); }\n")
	write("packages/web/src/lazy-routes.tsx", "import Lazy from './pages/Lazy';\n\nexport const routes = <Route path=\"/lazy\" element={<Lazy />} />;\n")
	g := BuildGraph(root)

	out := Context(g, []string{"packages/web/src/services/ordersApi.ts"}, 350, "")
	if !strings.Contains(out, "~") {
		t.Fatalf("expected an inferred page marker:\n%s", out)
	}
	if !strings.Contains(out, "inferred") {
		t.Fatalf("marker printed without a legend:\n%s", out)
	}
}

// A file imported from another module used to hand that module's endpoints to
// context: the foreign set was counted in, and labelled with this file's path.
// Context answers for the file; the module union is impact's job.
func TestContextKeepsForeignModulesOutOfFileEndpoints(t *testing.T) {
	root := copyFixture(t)
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("packages/backend/src/modules/orders/orders.domain.ts", "export const prisma = {} as any;\n")
	write("packages/backend/src/modules/reports/reports.routes.ts", `import { Router } from 'express';
import { prisma } from '../orders/orders.domain.js';

const router = Router();
router.get('/', async (_req, res) => {
  res.json(await prisma.order.findMany());
});
export default router;
`)
	// One importer in a foreign module is enough to pull the whole orders module in.
	write("packages/backend/src/modules/orders/orders.admin.routes.ts", `import { Router } from 'express';
import reportRoutes from '../reports/reports.routes.js';

const router = Router();
router.get('/', (_req, res) => res.json(reportRoutes));
export default router;
`)
	write("packages/backend/src/app.ts", `import express from 'express';
import orderRoutes from './modules/orders/orders.routes.js';
import adminRoutes from './modules/orders/orders.admin.routes.js';
import reportRoutes from './modules/reports/reports.routes.js';

const app = express();
app.use('/api/v1/orders', orderRoutes);
app.use('/api/v1/admin', adminRoutes);
app.use('/api/v1/reports', reportRoutes);
export default app;
`)
	target := "packages/backend/src/modules/reports/reports.routes.ts"
	g := BuildGraph(root)

	out := Context(g, []string{target}, 900, "")
	if !strings.Contains(out, "GET /api/v1/reports") {
		t.Fatalf("own endpoint missing:\n%s", out)
	}
	if strings.Contains(out, "GET /api/v1/orders") || strings.Contains(out, "GET /api/v1/admin") {
		t.Fatalf("foreign module endpoints leaked into context:\n%s", out)
	}
	if !strings.Contains(out, "reach") {
		t.Fatalf("module-wide count not reported:\n%s", out)
	}

	imp := Impact(g, []string{target})
	if !strings.Contains(imp, "GET /api/v1/orders") {
		t.Fatalf("impact lost the blast radius:\n%s", imp)
	}
}
