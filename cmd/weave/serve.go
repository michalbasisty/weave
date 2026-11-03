package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
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
	token := server.NewToken()

	// index once so the first paint is not a cold build
	if _, err := weave.Reindex(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

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

	addr := "127.0.0.1:" + strconv.Itoa(port)
	srv := &http.Server{Addr: addr, Handler: server.Handler(cfg), ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("weave serve: http://%s/?t=%s\n", addr, token)
	fmt.Println("  one page: the map, a Check button, file:line that opens, notes, the wiki under /wiki/")
	fmt.Println("  Ctrl+C stops it. Loopback only; it writes nothing outside .weave/ and the files you note.")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
