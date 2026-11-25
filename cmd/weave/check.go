package main

import (
	"fmt"
	"os"

	"github.com/michalbasisty/weave/internal/verify"
	"github.com/michalbasisty/weave/internal/weave"
)

func cmdCheck() int {
	markdown := arg("format", "") == "markdown" || hasFlag("markdown")
	g, err := weave.Reindex(root)
	if err != nil {
		// The graph could not be built: operational, not a contract failure. In
		// markdown mode the report must still carry the marker and a diagnosis,
		// because the Action posts and summarizes the file verbatim.
		res := verify.Result{Scope: "the graph could not be built", Operational: []string{err.Error()}}
		if markdown {
			fmt.Print(res.Markdown())
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 2
	}
	res := verify.Run(g, root, arg("against", ""), hasFlag("strict-tests"), pos)
	if markdown {
		fmt.Print(res.Markdown())
	} else {
		fmt.Println(res.Text())
	}
	return res.Code()
}
