# Panel smoke test

Needs Node 22+ (global `WebSocket`) and Chrome, Chromium or Edge. No packages to install.

```bash
node test/ui/panel.mjs          # build, serve a copy of the fixture, drive Chrome
node test/ui/panel.mjs --keep   # keep the temp repo and screenshots, print the path
node test/ui/panel.mjs --shots  # screenshots only, in the same temp dir
```

It builds `./cmd/weave`, copies `test/fixture` to a temp directory, adds
`test/ui/orders-spec.md` as `specs/capabilities/orders/spec.md`, starts `weave serve`
on a free port and drives headless Chrome over CDP. The checks:

- the menu order (Today, Map, Changes, Checks, Specs, Wiki) and the full-width Specs place;
- the Views table, its screen link and the owning capability;
- a capability opens as a rendered page (title, Parts table, raw link), its `edit` saves
  the Markdown, and back returns to the list;
- the gate runs and its verdict lands in the Check tab;
- a path chip opens the file window in the File tab;
- the wiki builds and renders inside the Wiki place;
- one tick rewrites the copied `tasks.md`.

Exit 1 on failure; every check prints a line. The frozen TypeScript parity fixture
(`test/fixture` with its goldens) is not modified: the capability spec and the tick are
added to the copy.
