package main

import (
	"fmt"
	"os"

	"github.com/michalbasisty/weave/internal/verify"
	"github.com/michalbasisty/weave/internal/weave"
)

func cmdCheck() int {
	g, err := weave.Reindex(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	res := verify.Run(g, root, arg("against", ""), hasFlag("strict-tests"), pos)
	if arg("format", "") == "markdown" || hasFlag("markdown") {
		fmt.Print(res.Markdown())
	} else {
		fmt.Println(res.Text())
	}
	if len(res.Errors) > 0 {
		return 1
	}
	return 0
}
