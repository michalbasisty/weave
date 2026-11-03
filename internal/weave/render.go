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

//go:embed assets/wiki.css
var wikiCSS string

//go:embed assets/wiki.js
var wikiJS string

//go:embed assets/explorer.js
var explorerJS string

// NamedGraph is one repository and the name the page shows for it.
type NamedGraph struct {
	Name  string
	Graph *Graph
	// Delta, when set, is what changed since a git ref; the page shows it first.
	Delta *ExploreDelta
}

// RenderExplore is renderExplore(projects): one self-contained page.
func RenderExplore(projects []NamedGraph) string {
	payload := make([]jsjson.Value, 0, len(projects))
	for _, ng := range projects {
		p := ExplorePayload(ng.Graph)
		p.Name = ng.Name
		p.Delta = ng.Delta
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
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Crect width='16' height='16' rx='3' fill='%2314233f'/%3E%3Cpath d='M2 8h12' stroke='%23fff' stroke-width='1.6'/%3E%3Ccircle cx='4' cy='8' r='2' fill='%2314233f' stroke='%237aa2f2' stroke-width='1.4'/%3E%3Ccircle cx='12' cy='8' r='2' fill='%2314233f' stroke='%23c992dc' stroke-width='1.4'/%3E%3C/svg%3E">
<script>try{var p=localStorage.getItem('weave:theme')||'auto',d=p==='dark'||(p==='auto'&&matchMedia('(prefers-color-scheme:dark)').matches);document.documentElement.dataset.theme=d?'dark':'light'}catch(e){try{document.documentElement.dataset.theme=matchMedia('(prefers-color-scheme:dark)').matches?'dark':'light'}catch(e2){}}</script>
<style>`

const afterCSS = `</style></head>
<body>
<a class="skip" href="#main">Skip to content</a>
<header class="top">
  <a class="brand" href="#today">weave</a>
  <select id="proj" aria-label="Project" hidden></select>
  <nav class="tabs" id="tabs" aria-label="Places"></nav>
  <div class="find" role="search">
    <svg class="ico" viewBox="0 0 16 16" aria-hidden="true"><circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5 14 14"/></svg>
    <label class="sr" for="q">Find a screen, endpoint, table or change</label>
    <input id="q" type="search" placeholder="Search the map" autocomplete="off" spellcheck="false">
    <kbd class="key" aria-hidden="true">/</kbd>
  </div>
  <button id="theme" class="themebtn" type="button"><svg class="ico" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.5"/><path d="M8 2.5v11a5.5 5.5 0 0 0 0-11z" class="fill"/></svg><span id="themelabel"></span></button>
</header>
<nav class="crumbs" id="crumbs" aria-label="You are here"></nav>
<main id="main" tabindex="-1">
  <section id="list" aria-label="List"></section>
  <section id="detail" aria-label="Item"></section>
</main>
<p class="sr" id="live" aria-live="polite"></p>
<script>const D=`

const afterJS = `</script>
</body></html>`
