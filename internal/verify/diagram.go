package verify

import (
	"fmt"
	"sort"
	"strings"

	"github.com/michalbasisty/weave/internal/weave"
)

// The blast-radius picture in a PR comment: the screens that call what the change
// touched, those endpoints, the tables behind them, and every removed endpoint a
// call still names, with the route it most likely meant. GitHub draws Mermaid
// blocks itself, so the reader needs nothing installed.

const (
	maxScreens   = 10
	maxEndpoints = 10
	maxTables    = 8
)

type diagram struct {
	screens, endpoints, tables             []string
	calls                                  map[[2]string]bool // screen -> endpoint
	uses                                   map[[2]string]bool // endpoint -> table
	broken                                 []brokenEdge
	moreScreens, moreEndpoints, moreTables int
}

type brokenEdge struct {
	gone, closest string
	screens       []string
}

// blastDiagram builds the picture from the graph and what the gate found. It
// returns "" when there is nothing to draw.
func blastDiagram(g *weave.Graph, f *weave.Facts, broken []Broken) string {
	d := diagram{calls: map[[2]string]bool{}, uses: map[[2]string]bool{}}

	// The endpoints the changed files declare or call; a shared module that
	// declares none reaches through its importers.
	var eps []*weave.GNode
	if f != nil {
		eps = f.FileEndpoints
		if len(eps) == 0 {
			eps = f.ViaEndpoints
		}
	}
	touched := map[string]bool{}
	for _, e := range eps {
		if e != nil && !touched[e.Name] {
			touched[e.Name] = true
			d.endpoints = append(d.endpoints, e.Name)
		}
	}
	sort.Strings(d.endpoints)
	if len(d.endpoints) > maxEndpoints {
		d.moreEndpoints = len(d.endpoints) - maxEndpoints
		d.endpoints = d.endpoints[:maxEndpoints]
	}
	shown := map[string]bool{}
	for _, e := range d.endpoints {
		shown[e] = true
	}

	byID := make(map[string]*weave.GNode, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	name := func(id string) string {
		if n := byID[id]; n != nil {
			return n.Name
		}
		_, rest, _ := strings.Cut(id, ":")
		return rest
	}

	// Screens that call a shown endpoint.
	screenSet := map[string]bool{}
	moduleOf := map[string][]string{} // endpoint id -> module ids
	usesModel := map[string][]string{}
	tableOf := map[string][]string{}
	for _, e := range g.Edges {
		switch e.Kind {
		case "calls":
			if strings.HasPrefix(e.From, "page:") && strings.HasPrefix(e.To, "endpoint:") && shown[name(e.To)] {
				s := name(e.From)
				screenSet[s] = true
				d.calls[[2]string{s, name(e.To)}] = true
			}
		case "in_module":
			moduleOf[e.From] = append(moduleOf[e.From], e.To)
		case "uses":
			usesModel[e.From] = append(usesModel[e.From], e.To)
		case "maps_to":
			tableOf[e.From] = append(tableOf[e.From], e.To)
		}
	}
	for _, b := range broken {
		be := brokenEdge{gone: b.Endpoint, closest: weave.Closest(g, b.Endpoint)}
		for _, s := range b.Screens {
			be.screens = append(be.screens, s)
			screenSet[s] = true
		}
		d.broken = append(d.broken, be)
	}
	for s := range screenSet {
		d.screens = append(d.screens, s)
	}
	sort.Strings(d.screens)
	if len(d.screens) > maxScreens {
		d.moreScreens = len(d.screens) - maxScreens
		d.screens = d.screens[:maxScreens]
	}

	// Tables behind each endpoint: endpoint → module → model → table.
	tableSet := map[string]bool{}
	for _, ep := range d.endpoints {
		for _, m := range moduleOf["endpoint:"+ep] {
			for _, model := range usesModel[m] {
				for _, t := range tableOf[model] {
					tn := name(t)
					d.uses[[2]string{ep, tn}] = true
					tableSet[tn] = true
				}
			}
		}
	}
	for t := range tableSet {
		d.tables = append(d.tables, t)
	}
	sort.Strings(d.tables)
	if len(d.tables) > maxTables {
		d.moreTables = len(d.tables) - maxTables
		d.tables = d.tables[:maxTables]
	}
	if len(d.endpoints)+len(d.broken) == 0 {
		return ""
	}
	return d.mermaid()
}

func (d diagram) mermaid() string {
	var b strings.Builder
	ids := map[string]string{}
	id := func(prefix, key string) string {
		k := prefix + key
		if v, ok := ids[k]; ok {
			return v
		}
		v := fmt.Sprintf("%s%d", prefix, len(ids))
		ids[k] = v
		return v
	}
	b.WriteString("```mermaid\nflowchart LR\n")
	b.WriteString("  classDef screen fill:#f6f8fa,stroke:#57606a,color:#24292f\n")
	b.WriteString("  classDef touched fill:#fff8c5,stroke:#9a6700,color:#3b2300\n")
	b.WriteString("  classDef gone fill:#ffebe9,stroke:#cf222e,color:#82071e,stroke-dasharray:4 3\n")
	b.WriteString("  classDef closest fill:#dafbe1,stroke:#1a7f37,color:#0f3d1c\n")
	b.WriteString("  classDef table fill:#ddf4ff,stroke:#0969da,color:#0a3069\n")

	screenShown := map[string]bool{}
	if len(d.screens) > 0 {
		b.WriteString("  subgraph S[\"Screens\"]\n")
		for _, s := range d.screens {
			screenShown[s] = true
			fmt.Fprintf(&b, "    %s[\"%s\"]:::screen\n", id("s", s), label(s))
		}
		if d.moreScreens > 0 {
			fmt.Fprintf(&b, "    smore[\"+%d more\"]:::screen\n", d.moreScreens)
		}
		b.WriteString("  end\n")
	}
	b.WriteString("  subgraph E[\"Endpoints\"]\n")
	for _, e := range d.endpoints {
		fmt.Fprintf(&b, "    %s[\"%s\"]:::touched\n", id("e", e), label(e))
	}
	if d.moreEndpoints > 0 {
		fmt.Fprintf(&b, "    emore[\"+%d more\"]:::touched\n", d.moreEndpoints)
	}
	for _, x := range d.broken {
		fmt.Fprintf(&b, "    %s[\"%s<br/><i>removed</i>\"]:::gone\n", id("x", x.gone), label(x.gone))
		if x.closest != "" && ids["e"+x.closest] == "" {
			fmt.Fprintf(&b, "    %s[\"%s\"]:::closest\n", id("e", x.closest), label(x.closest))
		}
	}
	b.WriteString("  end\n")
	if len(d.tables) > 0 {
		b.WriteString("  subgraph T[\"Tables\"]\n")
		for _, t := range d.tables {
			fmt.Fprintf(&b, "    %s[(\"%s\")]:::table\n", id("t", t), label(t))
		}
		if d.moreTables > 0 {
			fmt.Fprintf(&b, "    tmore[\"+%d more\"]:::table\n", d.moreTables)
		}
		b.WriteString("  end\n")
	}

	var calls [][2]string
	for c := range d.calls {
		if screenShown[c[0]] {
			calls = append(calls, c)
		}
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i][0]+calls[i][1] < calls[j][0]+calls[j][1] })
	links := 0
	var red, green []string
	for _, c := range calls {
		fmt.Fprintf(&b, "  %s --> %s\n", ids["s"+c[0]], ids["e"+c[1]])
		links++
	}
	for _, x := range d.broken {
		first := true
		for _, s := range x.screens {
			if !screenShown[s] {
				continue
			}
			if first { // one label per removed endpoint, so the picture stays readable
				fmt.Fprintf(&b, "  %s -. still calls .-x %s\n", ids["s"+s], ids["x"+x.gone])
				first = false
			} else {
				fmt.Fprintf(&b, "  %s -.-x %s\n", ids["s"+s], ids["x"+x.gone])
			}
			red = append(red, fmt.Sprint(links))
			links++
		}
		if x.closest != "" {
			fmt.Fprintf(&b, "  %s -. did you mean .-> %s\n", ids["x"+x.gone], ids["e"+x.closest])
			green = append(green, fmt.Sprint(links))
			links++
		}
	}
	var uses [][2]string
	for u := range d.uses {
		if ids["t"+u[1]] != "" {
			uses = append(uses, u)
		}
	}
	sort.Slice(uses, func(i, j int) bool { return uses[i][0]+uses[i][1] < uses[j][0]+uses[j][1] })
	for _, u := range uses {
		fmt.Fprintf(&b, "  %s --> %s\n", ids["e"+u[0]], ids["t"+u[1]])
	}
	if len(red) > 0 {
		fmt.Fprintf(&b, "  linkStyle %s stroke:#cf222e,stroke-width:2px\n", strings.Join(red, ","))
	}
	if len(green) > 0 {
		fmt.Fprintf(&b, "  linkStyle %s stroke:#1a7f37,stroke-width:2px\n", strings.Join(green, ","))
	}
	for _, sg := range []string{"S", "E", "T"} {
		if (sg == "S" && len(d.screens) == 0) || (sg == "T" && len(d.tables) == 0) {
			continue
		}
		fmt.Fprintf(&b, "  style %s fill:transparent,stroke:#d0d7de\n", sg)
	}
	b.WriteString("```\n")
	return b.String()
}

// label makes a name safe inside a quoted Mermaid label.
func label(s string) string {
	return strings.NewReplacer(`"`, "#quot;", "<", "#lt;", ">", "#gt;").Replace(s)
}
