package weave

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"

	"github.com/michalbasisty/weave/internal/jsjson"
)

// The evaluated CSS and JS from src/render.ts, dumped by test/parity/dump-assets.mjs.
// Both implementations must ship these exact bytes.
//
//go:embed assets/explorer.css
var explorerCSS string

//go:embed assets/explorer.js
var explorerJS string

// NamedGraph is one repository and the name the page shows for it.
type NamedGraph struct {
	Name  string
	Graph *Graph
}

// RenderExplore is renderExplore(projects): one self-contained page.
func RenderExplore(projects []NamedGraph) string {
	payload := make([]jsjson.Value, 0, len(projects))
	for _, ng := range projects {
		p := ExplorePayload(ng.Graph)
		p.Name = ng.Name
		for i := range p.Changes {
			s := ChangeSummary(ng.Graph, strings.TrimPrefix(p.Changes[i].C, "change:"))
			p.Changes[i].S = &s
		}
		payload = append(payload, p.value())
	}
	root := &jsjson.Object{}
	root.Set("projects", jsjson.Arr(payload...))
	// JSON.stringify(...).replace(/</g, '\\u003c'): once, and no HTML escaping.
	data := bytes.ReplaceAll(jsjson.Marshal(jsjson.Obj(root)), []byte("<"), []byte(`\u003c`))

	title := fmt.Sprintf("%d projects", len(projects))
	if len(projects) == 1 {
		title = projects[0].Name
	}

	var b strings.Builder
	b.Grow(len(data) + len(explorerCSS) + len(explorerJS) + 2048)
	b.WriteString(headStart)
	b.WriteString(esc(title))
	b.WriteString(headAfterTitle)
	b.WriteString(explorerCSS)
	b.WriteString(afterCSS)
	b.Write(data)
	b.WriteString(";")
	b.WriteString(explorerJS)
	b.WriteString(afterJS)
	return b.String()
}

var escReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// esc is src/render.ts's esc: exactly four replacements, in that order.
func esc(s string) string { return escReplacer.Replace(s) }

const headStart = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>weave — `

const headAfterTitle = `</title>
<meta name="description" content="weave maps the screens, API endpoints, modules and tables of one repository, with the tests and spec changes around them.">
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Crect width='16' height='16' fill='%23f4f1e8'/%3E%3Cpath d='M3 5h10M3 8h10M3 11h6' stroke='%231b1a17' stroke-width='1.6'/%3E%3C/svg%3E">
<script>try{var p=localStorage.getItem('weave:theme')||'auto',d=p==='dark'||(p==='auto'&&matchMedia('(prefers-color-scheme:dark)').matches);document.documentElement.dataset.theme=d?'dark':'light'}catch(e){try{document.documentElement.dataset.theme=matchMedia('(prefers-color-scheme:dark)').matches?'dark':'light'}catch(e2){}}</script>
<style>`

const afterCSS = `</style></head>
<body>
<a class="skip" href="#list">Skip to the list</a>
<header class="top">
  <span class="brand">weave</span>
  <select id="proj" aria-label="Project"></select>
  <span class="spacer"></span>
  <button id="theme" class="tbtn" type="button" aria-label="Theme"></button>
</header>
<div class="shell">
  <nav class="rail" aria-label="Views">
    <div class="find">
      <label for="q">Find anything</label>
      <input id="q" type="search" placeholder="Find anything: route, table, change…" autocomplete="off" spellcheck="false">
    </div>
    <div id="nav"></div>
  </nav>
  <main>
    <section id="list" aria-label="Results" tabindex="-1"></section>
    <section id="detail" aria-label="Detail"></section>
  </main>
</div>
<script>const D=`

const afterJS = `</script>
</body></html>`
