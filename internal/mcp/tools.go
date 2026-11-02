package mcp

import "encoding/json"

// Tool schemas are ordered by hand: tools/list is compared byte for byte with the
// TypeScript server, and a Go map would sort the properties.

type propDef struct {
	Type        string   `json:"type,omitempty"`
	Items       *propDef `json:"items,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`
}

type prop struct {
	Name string
	Def  propDef
}

type props []prop

func (p props) MarshalJSON() ([]byte, error) {
	b := []byte{'{'}
	for i, x := range p {
		if i > 0 {
			b = append(b, ',')
		}
		k, err := json.Marshal(x.Name)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(x.Def)
		if err != nil {
			return nil, err
		}
		b = append(b, k...)
		b = append(b, ':')
		b = append(b, v...)
	}
	return append(b, '}'), nil
}

type schema struct {
	Type       string   `json:"type"`
	Properties props    `json:"properties"`
	Required   []string `json:"required,omitempty"`
}

type tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema schema `json:"inputSchema"`
}

var toolDefs = []tool{
	{
		Name:        "context",
		Description: "START HERE before editing a file. One compact pack: what the file is (page/module), the endpoints it serves, tables, the spec change that owns it, open tasks, and the tests to run. Cheaper than reading files or grepping.",
		InputSchema: schema{Type: "object", Properties: props{
			{"files", propDef{Type: "array", Items: &propDef{Type: "string"}, Description: "repo-relative paths"}},
			{"budget", propDef{Type: "number", Description: "token budget, default 350"}},
			{"depth", propDef{Type: "string", Enum: []string{"names", "code"}, Description: "code: a short window at each anchor (opt-in)"}},
		}, Required: []string{"files"}},
	},
	{
		Name:        "impact",
		Description: "Full blast radius of a change: every module, endpoint, table, page, test and requirement it can reach, plus endpoints no test covers. Use when you need the complete list, not the summary.",
		InputSchema: schema{Type: "object", Properties: props{
			{"files", propDef{Type: "array", Items: &propDef{Type: "string"}, Description: "repo-relative paths"}},
		}, Required: []string{"files"}},
	},
	{
		Name:        "search",
		Description: "Find endpoints, pages, modules, tables, requirements and tests by name. One line per hit. With text: true, scan source lines for a literal string (or a regex) instead.",
		InputSchema: schema{Type: "object", Properties: props{
			{"query", propDef{Type: "string"}},
			{"limit", propDef{Type: "number"}},
			{"text", propDef{Type: "boolean", Description: "search source lines instead of graph names"}},
			{"regex", propDef{Type: "boolean", Description: "treat query as a regular expression (text search only)"}},
		}, Required: []string{"query"}},
	},
	{
		Name:        "remember",
		Description: "Keep a short note next to files for the next session: a decision, a gotcha, why a shape is odd. context() returns it, labelled as a note rather than evidence.",
		InputSchema: schema{Type: "object", Properties: props{
			{"files", propDef{Type: "array", Items: &propDef{Type: "string"}, Description: "repo-relative paths"}},
			{"note", propDef{Type: "string", Description: "at most a few sentences"}},
		}, Required: []string{"files", "note"}},
	},
	{
		Name:        "recall",
		Description: "Notes previously remembered for these files, newest first. Pass no files for the newest notes in this repo.",
		InputSchema: schema{Type: "object", Properties: props{
			{"files", propDef{Type: "array", Items: &propDef{Type: "string"}}},
		}},
	},
	{
		Name:        "plan",
		Description: "The next task of a spec change, with only what is needed to start it: its files, endpoints, tables, tests and the command to run them. Call this instead of reading tasks.md.",
		InputSchema: schema{Type: "object", Properties: props{
			{"slug", propDef{Type: "string", Description: "change record slug, e.g. 2026-10-22-warehouse-orders"}},
			{"task", propDef{Type: "string", Description: "task id (T3); omit for the next unfinished one"}},
		}, Required: []string{"slug"}},
	},
	{
		Name:        "ask",
		Description: "Ask a question about this repo in plain language. A cheap model answers from the graph in a few lines and names the facts it used. Use this instead of chaining search/context/impact yourself.",
		InputSchema: schema{Type: "object", Properties: props{
			{"question", propDef{Type: "string", Description: "e.g. \"what does the warehouse change touch?\""}},
			{"files", propDef{Type: "array", Items: &propDef{Type: "string"}, Description: "optional repo-relative paths to ground the answer"}},
		}, Required: []string{"question"}},
	},
	{
		Name:        "changes",
		Description: "A spec change record: its tasks, open tasks, the files its tasks touch, and the blast radius of those files. With no argument, lists all change records.",
		InputSchema: schema{Type: "object", Properties: props{
			{"slug", propDef{Type: "string"}},
		}},
	},
	{
		Name:        "changed",
		Description: "What the graph gained or lost since a git ref (default HEAD) or a saved graph.json: endpoints, pages, tables, modules, requirements. A removed endpoint still called by the base is named with its caller count.",
		InputSchema: schema{Type: "object", Properties: props{
			{"ref", propDef{Type: "string", Description: "git ref, default HEAD"}},
			{"from", propDef{Type: "string", Description: "path to a saved graph.json, relative to the repo"}},
			{"all", propDef{Type: "boolean", Description: "show up to 60 per section instead of 12"}},
		}},
	},
	{
		Name:        "status",
		Description: "Graph health: node counts, endpoints no page calls, endpoints no test covers, pages without e2e.",
		InputSchema: schema{Type: "object", Properties: props{}},
	},
}

func visibleTools(multi, manager bool) []tool {
	base := toolDefs
	if manager {
		base = nil
		for _, t := range toolDefs {
			if t.Name == "ask" || t.Name == "plan" || t.Name == "status" {
				base = append(base, t)
			}
		}
	}
	if !multi {
		return base
	}
	out := make([]tool, 0, len(base))
	for _, t := range base {
		cp := t
		cp.InputSchema.Properties = append(append(props{}, t.InputSchema.Properties...),
			prop{"project", propDef{Type: "string", Description: "project name from `weave projects`; omit for this repo"}})
		out = append(out, cp)
	}
	return out
}
