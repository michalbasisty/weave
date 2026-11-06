package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/server"
	"github.com/michalbasisty/weave/internal/weave"
)

// cmdServe hosts the page a person actually works in: the explorer, the wiki under
// /wiki/, and actions wired to the same CLI the agent uses. Loopback only.
func cmdServe() int {
	port := intArg("port", "7777")
	if port < 1024 || port > 65535 {
		fmt.Fprintln(os.Stderr, "weave serve: --port must be between 1024 and 65535")
		return 2
	}

	// index once so the first paint is not a cold build
	if _, err := weave.Reindex(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// The token is kept beside the index so a restart does not break every open tab's
	// write access. `--new-token` rotates it.
	token := serveToken(root, hasFlag("new-token"))

	cfg := server.Config{
		Root:  root,
		Token: token,
		Graph: func() (*weave.Graph, error) {
			if weave.IsFresh(root, "") {
				if g, err := weave.LoadGraph(root); err == nil {
					return g, nil
				}
			}
			return weave.Reindex(root)
		},
	}

	// Bind before printing a URL. A second `weave serve` used to print its link and
	// then fail on the busy port, so the link opened the first server's project.
	ln, err := listenFrom(port, arg("port", "") == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	addr := ln.Addr().String()
	srv := &http.Server{Handler: server.Handler(cfg), ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("weave serve: %s\n", root)
	fmt.Printf("  http://%s/?t=%s\n", addr, token)
	if p := ln.Addr().(*net.TCPAddr).Port; p != port {
		fmt.Printf("  port %d is taken (another weave serve?), so this one is on %d\n", port, p)
	}
	fmt.Println("  the token is kept in .weave/serve.token; --new-token rotates it")
	fmt.Println("  one page: the map, a Check button, file:line that opens, notes, specs, the wiki under /wiki/")
	fmt.Println("  Ctrl+C stops it. Loopback only; it writes nothing outside .weave/, specs/ and the files you note.")
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// serveToken reuses the token in .weave/serve.token so a restart keeps the URLs people
// have open; --new-token rotates it. A malformed file is replaced.
func serveToken(root string, rotate bool) string {
	path := filepath.Join(root, ".weave", "serve.token")
	if !rotate {
		if raw, err := os.ReadFile(path); err == nil {
			if t := strings.TrimSpace(string(raw)); validToken(t) {
				return t
			}
		}
	}
	t := server.NewToken()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(t+"\n"), 0o600)
	return t
}

func validToken(t string) bool {
	if len(t) != 32 {
		return false
	}
	for _, r := range t {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// listenFrom binds 127.0.0.1:port. When the port was not asked for by name and is
// busy, it tries the next twenty, so each project can have its own panel open.
func listenFrom(port int, mayMove bool) (net.Listener, error) {
	tries := 1
	if mayMove {
		tries = 20
	}
	var first error
	for p := port; p < port+tries && p <= 65535; p++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err == nil {
			return ln, nil
		}
		if first == nil {
			first = err
		}
	}
	if mayMove {
		return nil, fmt.Errorf("weave serve: ports %d-%d are all taken: %v", port, port+tries-1, first)
	}
	return nil, fmt.Errorf("weave serve: port %d is taken (another weave serve?): stop it, or pass a free --port: %v", port, first)
}
