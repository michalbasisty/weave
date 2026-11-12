package main

import (
	"encoding/json"
	"os"
	"strings"
)

const (
	agentStart = "<!-- weave:start -->"
	agentEnd   = "<!-- weave:end -->"
)

// agentBlock is what `weave init` puts in CLAUDE.md and AGENTS.md: when to use
// which tool, in the terms a task arrives in, and when grep is still right.
const agentBlock = agentStart + `
## Repo map (weave)

This repo has a weave MCP server. It knows which screen calls which endpoint, which
module serves it and which tables it touches, including URLs that are joined from a
router prefix and a route and so never appear whole in the source.

- A task names a screen, a URL or a behaviour, not a file: ` + "`search(\"plain words\")`" + ` first.
- About to open or edit files: ` + "`context(files)`" + ` once; it lists the endpoints, tables and tests.
- Renaming or removing a route, procedure or API call: ` + "`impact(files)`" + ` for every caller.
- Before saying done: ` + "`verify()`" + `; it names any call left without an endpoint.

Grep is still right for a symbol whose exact name you already know.
` + agentEnd + "\n"

// writeAgentBlock adds the weave section to an agent instructions file, creating
// the file if needed. An existing section is left alone unless upgrade is set.
// It returns what it did: "+= weave section", "(created)", "(upgraded)", "kept",
// "unwritable", or "" when nothing changed.
func writeAgentBlock(p string, upgrade bool) string {
	raw, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return "unwritable"
	}
	text := string(raw)
	i, j := strings.Index(text, agentStart), strings.Index(text, agentEnd)
	var out, did string
	switch {
	case i >= 0 && j > i:
		if !upgrade {
			return "kept"
		}
		end := j + len(agentEnd)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		out = text[:i] + agentBlock + text[end:]
		if out == text {
			return ""
		}
		did = "(upgraded)"
	case len(text) == 0:
		out, did = agentBlock, "(created)"
	default:
		out, did = strings.TrimRight(text, "\n")+"\n\n"+agentBlock, "+= weave section"
	}
	if os.WriteFile(p, []byte(out), 0o644) != nil {
		return "unwritable"
	}
	return did
}

// openCodePlugin is .opencode/plugins/weave.js: on session.idle it runs the gate
// and, when it fails with something new, shows a toast and hands the failure back
// to the session so the agent fixes it before the person reads "done".
func openCodePlugin(exe string) string {
	bin, _ := json.Marshal(exe)
	return `// Written by ` + "`weave init --hooks`" + `. Runs weave's gate when the agent stops and hands a
// failure back to it, the same as the Claude Code Stop hook. Works in opencode and in
// front ends built on it, such as OpenChamber. Delete this file to turn it off.
const WEAVE = ` + string(bin) + `

export const WeaveGate = async ({ client, $, directory }) => {
  let last = ""
  return {
    event: async ({ event }) => {
      if (event.type !== "session.idle") return
      const res = await $` + "`${WEAVE} check --root ${directory}`" + `.quiet().nothrow()
      if (res.exitCode === 0) { last = ""; return }
      const out = (String(res.stdout) + String(res.stderr)).trim()
      // say each failure once: the agent may stop again without fixing it
      if (!out || out === last) return
      last = out
      await client.tui?.showToast?.({ body: { message: "weave check failed", variant: "error" } })?.catch?.(() => {})
      const id = event.properties?.sessionID
      if (!id) return
      await client.session.prompt({
        path: { id },
        body: { parts: [{ type: "text", text: "weave check failed on your changes. Fix this before finishing:\n\n" + out }] },
      }).catch(() => {})
    },
  }
}
`
}
