package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/server"
	"github.com/michalbasisty/weave/internal/verify"
	"github.com/michalbasisty/weave/internal/weave"
)

// `weave demo` shows the gate on a sample shop in half a minute: map the app,
// let an "agent" rename an endpoint the front end still calls, watch the gate
// catch it with the route it meant, apply the fix, and pass.

//go:embed demoapp.txtar
var demoArchive string

// The demo's edit and its fix: a rename on the server, then the client caught up.
const (
	demoRoutes = "server/src/orders/orders.routes.ts"
	demoClient = "web/src/api/orders.ts"
)

var demoEdits = []struct{ file, from, to string }{
	{demoRoutes, "router.get('/', ", "router.get('/history', "},
	{demoClient, "http.get('/api/orders')", "http.get('/api/orders/history')"},
}

// unpackDemo writes the archive's files under dir.
func unpackDemo(dir string) error {
	var name string
	var body strings.Builder
	flush := func() error {
		if name == "" {
			return nil
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(body.String()), 0o644)
	}
	for _, line := range strings.SplitAfter(demoArchive, "\n") {
		t := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(t, "-- ") && strings.HasSuffix(t, " --") && len(t) > 6 {
			if err := flush(); err != nil {
				return err
			}
			name = t[3 : len(t)-3]
			body.Reset()
			continue
		}
		if name != "" {
			body.WriteString(line)
		}
	}
	return flush()
}

func demoEdit(dir string, i int) error {
	e := demoEdits[i]
	p := filepath.Join(dir, filepath.FromSlash(e.file))
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if !strings.Contains(string(raw), e.from) {
		return fmt.Errorf("demo: %s no longer contains %q", e.file, e.from)
	}
	// A visible mtime step, so the live map's poll sees the edit even on a coarse clock.
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), e.from, e.to, 1)), 0o644); err != nil {
		return err
	}
	later := time.Now().Add(time.Second)
	_ = os.Chtimes(p, later, later)
	return nil
}

func demoGit(dir string, args ...string) error {
	full := append([]string{"-C", dir, "-c", "user.name=weave demo", "-c", "user.email=demo@weave.invalid", "-c", "commit.gpgsign=false"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

func cmdDemo() int {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, "weave demo needs git on your PATH: the gate compares your change with the last commit")
		return 1
	}
	dir := ""
	if len(pos) > 0 {
		dir = pos[0]
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			fmt.Fprintf(os.Stderr, "weave demo: %s is not empty; give a new folder, or none for a temporary one\n", dir)
			return 2
		}
	} else {
		d, err := os.MkdirTemp("", "weave-demo-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		dir = d
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dir = abs
	return runDemo(dir, hasFlag("live"), os.Stdout)
}

func runDemo(dir string, live bool, out *os.File) int {
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := unpackDemo(dir); err != nil {
		return fail(err)
	}
	if err := demoGit(dir, "init", "-q"); err != nil {
		return fail(err)
	}
	if err := demoGit(dir, "add", "-A"); err != nil {
		return fail(err)
	}
	if err := demoGit(dir, "commit", "-q", "-m", "A small shop"); err != nil {
		return fail(err)
	}

	bold := func(s string) string { return s }
	if isTerminal(out) {
		bold = func(s string) string { return "\033[1m" + s + "\033[0m" }
	}
	step := func(n int, title string) {
		fmt.Fprintf(out, "\n%s\n", bold(fmt.Sprintf("%d. %s", n, title)))
	}

	g, err := weave.Reindex(dir)
	if err != nil {
		return fail(err)
	}
	counts := map[string]int{}
	for _, n := range g.Nodes {
		counts[n.Kind]++
	}
	fmt.Fprintf(out, "weave demo: a small shop in %s\n", dir)
	step(1, "weave maps the app")
	fmt.Fprintf(out, "   %d screens, %d endpoints, %d tables, read from the code in %s\n",
		counts[weave.KindPage], counts[weave.KindEndpoint], counts["table"], elapsed(g))
	if fa := weave.Analyze(g, []string{demoRoutes}); fa != nil {
		var screens []string
		for _, p := range fa.Pages {
			screens = append(screens, p.Name)
		}
		fmt.Fprintf(out, "   %s serves %d endpoints; the screens %s call them\n", demoRoutes, len(fa.FileEndpoints), strings.Join(screens, " and "))
	}

	var wait func()
	if live {
		token := serveToken(dir, false)
		ln, err := listenFrom(7777, arg("port", "") == "")
		if err != nil {
			return fail(err)
		}
		cfg := projectConfig(dir, token)
		srv := &http.Server{Handler: server.Handler(cfg), ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		url := "http://" + ln.Addr().String() + "/live?t=" + token
		fmt.Fprintf(out, "   live map: %s\n", url)
		if !hasFlag("no-open") {
			openBrowser(url)
		}
		in := bufio.NewReader(os.Stdin)
		tty := isTerminal(os.Stdin)
		wait = func() {
			if tty {
				fmt.Fprint(out, "   press Enter for the next step ")
				_, _ = in.ReadString('\n')
			} else {
				time.Sleep(5 * time.Second)
			}
		}
		wait()
	}

	step(2, "An agent renames an endpoint")
	fmt.Fprintf(out, "   %s: GET /api/orders is now GET /api/orders/history\n", demoRoutes)
	fmt.Fprintln(out, "   The server builds and the diff looks clean. The front end still calls the old path.")
	if err := demoEdit(dir, 0); err != nil {
		return fail(err)
	}
	if wait != nil {
		wait()
	}

	step(3, "weave check")
	res, err := demoCheck(dir)
	if err != nil {
		return fail(err)
	}
	printVerdict(out, res)
	if len(res.Errors) == 0 {
		return fail(fmt.Errorf("weave demo: the gate should have failed here"))
	}
	for _, b := range res.Broken {
		fmt.Fprintf(out, "   Screens %s call %s through %s, and that route is gone.\n", strings.Join(b.Screens, " and "), b.Endpoint, b.Caller)
		if b.Closest != "" {
			fmt.Fprintf(out, "   weave names the route it most likely meant: %s\n", b.Closest)
		}
	}
	if wait != nil {
		wait()
	}

	step(4, "The fix: the client calls the new route")
	fmt.Fprintf(out, "   %s: /api/orders → /api/orders/history\n", demoClient)
	if err := demoEdit(dir, 1); err != nil {
		return fail(err)
	}
	res, err = demoCheck(dir)
	if err != nil {
		return fail(err)
	}
	printVerdict(out, res)
	if len(res.Errors) > 0 {
		return fail(fmt.Errorf("weave demo: the gate should pass after the fix"))
	}

	fmt.Fprintf(out, "\n%s\n", bold("That is the gate an agent hits before it says done."))
	fmt.Fprintln(out, "   On your app:  cd <your repo> && weave init --hooks   (Claude Code, opencode)")
	fmt.Fprintln(out, "   In CI:        uses: michalbasisty/weave@main   (a PR comment with the blast radius)")
	fmt.Fprintf(out, "   This shop:    cd %s && weave live\n", dir)
	if live {
		fmt.Fprintln(out, "\n   The live map stays up; edit the shop and watch it. Ctrl+C stops it.")
		select {}
	}
	return 0
}

func demoCheck(dir string) (verify.Result, error) {
	g, err := weave.Reindex(dir)
	if err != nil {
		return verify.Result{}, err
	}
	return verify.Run(g, dir, "", false, nil), nil
}

// printVerdict shows the scope and the failures; the demo leaves out the advice
// lines (`warn`, `run`) that `weave check` also prints.
func printVerdict(out *os.File, res verify.Result) {
	fmt.Fprintf(out, "   check: %s\n", res.Scope)
	for _, e := range res.Errors {
		fmt.Fprintf(out, "   FAIL %s\n", e)
	}
	if len(res.Errors) == 0 {
		fmt.Fprintln(out, "   PASS: the front end and the API agree again")
	}
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
