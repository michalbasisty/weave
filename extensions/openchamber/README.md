# weave for OpenChamber

A **weave gate** section in OpenChamber's Work Status. It runs
[weave](https://github.com/michalbasisty/weave)'s contract gate on the open project and
shows what the uncommitted change broke: every front-end call whose endpoint no longer
exists, the route it most likely meant, where the call is, and the tests to run.

```
gate      1 file(s) → 1 endpoint(s), 1 endpoint(s) removed        1 broken
GET /tags closest now: GET /tag-list            frontend/src/agent.js:218
tests     cd backend && npx jest src/tests/services/tag.service.test.ts
```

It re-runs when the tree changes (HEAD or `git status` moves) and at least once a minute,
and **Re-run** in the section header runs it now.

## Install

1. Install weave: `go install github.com/michalbasisty/weave/cmd/weave@latest`, or a binary
   from [Releases](https://github.com/michalbasisty/weave/releases).
2. In OpenChamber, **Settings → Extensions**, add this folder (or its git URL, or the
   `weave-openchamber.zip` from a release).
3. Approve the local service. It runs `weave check` and `git status` on the project you have
   open, nothing else, and has no dependencies.

The service looks for `weave` on `PATH`, then in `~/go/bin`, `~/.local/bin`, `~/bin`,
`/opt/homebrew/bin` and `/usr/local/bin`, because a desktop app's `PATH` often lacks the
folder `go install` writes to.

## Give the agent weave too

The section is for you. For the agent, run `weave init --hooks` in the project: it writes
`opencode.json` (weave's MCP tools for every OpenCode session, OpenChamber's included), a
"when to use weave" section in `AGENTS.md`, and `.opencode/plugins/weave.js`, which runs the
same gate when the agent stops and hands a failure back to it.

## Files

- `package.json`: the manifest. A Work Status section the app draws from `GET /status`, and
  the local service. No panel page, no bundle.
- `service/main.js`: the service. Plain Node, no packages.

Checked with `openchamber-guest-check` from `@openchamber/sdk` 2.7.0, and its answers with the
SDK's `guestStatusViewSchema`.
