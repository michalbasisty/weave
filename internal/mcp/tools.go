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
		Description: "Call before opening or editing files a task is about. One compact pack per file: the page or module it is, the endpoints it serves and calls, tables, the owning spec change, open tasks, and the tests to run. Usually replaces reading the file and the files around it.",
		InputSchema: schema{Type: "object", Properties: props{
			{"files", propDef{Type: "array", Items: &propDef{Type: "string"}, Description: "repo-relative paths"}},
			{"budget", propDef{Type: "number", Description: "token budget, default 350"}},
			{"depth", propDef{Type: "string", Enum: []string{"names", "code"}, Description: "code: a short window at each anchor (opt-in)"}},
		}, Required: []string{"files"}},
	},
	{
		Name:        "impact",
		Description: "Before renaming, moving or removing a route, procedure or API call: every page, client call, module, table, test and requirement that reaches it, in one list, so you do not grep for each caller. Also names endpoints no test covers.",
		InputSchema: schema{Type: "object", Properties: props{
			{"files", propDef{Type: "array", Items: &propDef{Type: "string"}, Description: "repo-relative paths"}},
		}, Required: []string{"files"}},
	},
	{
		Name:        "search",
		Description: "Find where something lives when the task describes it instead of naming a file: plain words (\"inbox count\", \"favorite an article\") or a URL. Matches endpoints, pages, modules, tables, requirements and tests, one line per hit, including URLs that exist only once a router prefix and a route are joined, which grep cannot find. With text: true, scan source lines for a literal string (or a regex) instead.",
		InputSchema: schema{Type: "object", Properties: props{
			{"query", propDef{Type: "string"}},
			{"limit", propDef{Type: "number"}},
			{"text", propDef{Type: "boolean", Description: "search source lines instead of graph names"}},
			{"regex", propDef{Type: "boolean", Description: "treat query as a regular expression (text search only)"}},
		}, Required: []string{"query"}},
	},
	{
		Name:        "remember",
		Description: "Keep a short note next to files for the next session: a decision, a gotcha, why a shape is odd. Not what the map shows (owner, module, endpoints): context() has that. Returned as a note, not evidence.",
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
		Name:        "task_done",
		Description: "Tick one task in a change record's tasks.md, the same edit as `weave spec task done`. plan(slug) returns the id; do not edit tasks.md yourself.",
		InputSchema: schema{Type: "object", Properties: props{
			{"change", propDef{Type: "string", Description: "change record slug, e.g. 2026-10-22-warehouse-orders"}},
			{"task", propDef{Type: "string", Description: "task id from plan, e.g. T3"}},
		}, Required: []string{"change", "task"}},
	},
	{
		Name:        "task_reopen",
		Description: "Reopen one task in a change record's tasks.md, the same edit as `weave spec task reopen`. Use it when a ticked task turns out unfinished.",
		InputSchema: schema{Type: "object", Properties: props{
			{"change", propDef{Type: "string", Description: "change record slug, e.g. 2026-10-22-warehouse-orders"}},
			{"task", propDef{Type: "string", Description: "task id, e.g. T3"}},
		}, Required: []string{"change", "task"}},
	},
	{
		Name:        "change_new",
		Description: "Create a change record under specs/changes: proposal.md, tasks.md and spec-delta.md, the same files as `weave spec new`. Returns the slug; plan(slug) then gives the first task.",
		InputSchema: schema{Type: "object", Properties: props{
			{"name", propDef{Type: "string", Description: "slug, e.g. billing-retry; today's date is prefixed unless present"}},
			{"title", propDef{Type: "string", Description: "human title; defaults to the name"}},
		}, Required: []string{"name"}},
	},
	{
		Name:        "change_delete",
		Description: "Delete one change record and its three files. Irreversible: pass the exact change and confirm must be true.",
		InputSchema: schema{Type: "object", Properties: props{
			{"change", propDef{Type: "string", Description: "change record slug"}},
			{"confirm", propDef{Type: "boolean", Description: "must be true; the directory is removed"}},
		}, Required: []string{"change", "confirm"}},
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
		Name:        "verify",
		Description: "Run the gate on the current diff: broken calls, untested endpoints, the owning change, and the exact commands to run the tests. Call it before saying done.",
		InputSchema: schema{Type: "object", Properties: props{
			{"files", propDef{Type: "array", Items: &propDef{Type: "string"}, Description: "repo-relative paths; default: the git diff"}},
			{"against", propDef{Type: "string", Description: "git ref to compare against"}},
			{"strict", propDef{Type: "boolean", Description: "an untested endpoint fails the gate"}},
		}},
	},
	{
		Name:        "status",
		Description: "Graph health: node counts, endpoints no page calls, endpoints no test covers, pages without e2e.",
		InputSchema: schema{Type: "object", Properties: props{}},
	},
}

// coreTools are in every session. The usage log: context took 62% of calls, and
// no tool outside these five reached three.
var coreTools = map[string]bool{"context": true, "impact": true, "search": true, "changed": true, "verify": true}

var specTools = map[string]bool{"plan": true, "changes": true, "task_done": true, "task_reopen": true, "change_new": true, "change_delete": true}

func visibleTools(multi, manager, specs, full bool) []tool {
	base := make([]tool, 0, len(toolDefs))
	for _, t := range toolDefs {
		switch {
		case manager:
			if t.Name != "ask" && t.Name != "plan" && t.Name != "status" {
				continue
			}
		case t.Name == "ask":
			// ask stays a CLI command: the usage log never called it, and a tool that
			// costs a model call should not sit in every session's schema.
			continue
		case full, coreTools[t.Name]:
		case specTools[t.Name]:
			if !specs {
				continue
			}
		default:
			// remember, recall, status: notes are for people (weave remember, the
			// panel), and status repeats the panel's overview
			continue
		}
		base = append(base, t)
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
