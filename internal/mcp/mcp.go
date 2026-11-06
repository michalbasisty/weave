package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/baseline"
	"github.com/michalbasisty/weave/internal/delta"
	"github.com/michalbasisty/weave/internal/grep"
	"github.com/michalbasisty/weave/internal/usage"
	"github.com/michalbasisty/weave/internal/verify"
	"github.com/michalbasisty/weave/internal/version"
	"github.com/michalbasisty/weave/internal/weave"
)

// MCP over stdio, newline-delimited JSON-RPC. No SDK, no dependencies.

const protocol = "2024-11-05"

var supported = []string{"2024-11-05", "2025-03-26", "2025-06-18"}

const instructions = "weave maps a repo: UI pages -> HTTP endpoints -> modules -> database tables, plus spec change records and tests. " +
	"If the work comes from a spec change, call plan(slug) first: it returns the next task with its files, endpoints and the command to run. " +
	"When that task is finished, call task_done(change, task) instead of editing tasks.md; task_reopen(change, task) undoes a tick. " +
	"Start a change with change_new(name); change_delete(change, confirm: true) removes one and cannot be undone. " +
	"Before editing any other file, call context(files) once: page, endpoints, tables, owning change, open tasks, requirements, " +
	"tests to run and the command to run them, in a few hundred tokens. Call impact(files) only when you need the complete list. " +
	"Use search() instead of grepping directories; search(text: true) scans source lines when you need a literal string. " +
	"changed() tells you what the graph gained or lost since a branch point. " +
	"Call remember(files, note) after you learn something the next session needs; context() returns those notes, labelled as claims, not evidence. " +
	"context(depth: \"code\") adds a short window at each anchor when you need the exact lines. " +
	"Before saying a task is done, call verify(): it runs the gate on the diff and returns the commands that prove it. " +
	"No tool returns whole files; text search returns matching lines only, and every output is capped."

type GraphSource func(project string) (*weave.Graph, error)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type capabilities struct {
	Tools     struct{} `json:"tools"`
	Resources struct{} `json:"resources"`
	Prompts   struct{} `json:"prompts"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
	Instructions    string       `json:"instructions"`
}

type runtimeStats struct {
	start time.Time
	calls int
	ms    []float64
}

var runtime = runtimeStats{start: time.Now()}

// Serve runs the JSON-RPC loop until stdin closes. invalidate, when set, tells the
// caller its cached graph is stale after a write.
func Serve(src GraphSource, multi, manager bool, invalidate func()) error {
	return ServeIO(os.Stdin, os.Stdout, src, multi, manager, invalidate)
}

// ServeIO is Serve with explicit streams, for tests.
func ServeIO(in io.Reader, out io.Writer, src GraphSource, multi, manager bool, invalidate func()) error {
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	defer writer.Flush()
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			handle(strings.TrimSpace(line), src, multi, manager, invalidate, writer)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func send(w *bufio.Writer, msg response) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(msg); err != nil {
		return
	}
	_, _ = w.Write(buf.Bytes())
	_ = w.Flush()
}

func handle(line string, src GraphSource, multi, manager bool, invalidate func(), w *bufio.Writer) {
	if line == "" {
		return
	}
	var msg rpcMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}
	if len(msg.ID) == 0 || string(msg.ID) == "null" {
		return // notification
	}
	reply := func(result any) { send(w, response{JSONRPC: "2.0", ID: msg.ID, Result: result}) }
	fail := func(code int, message string) {
		send(w, response{JSONRPC: "2.0", ID: msg.ID, Error: &rpcError{Code: code, Message: message}})
	}

	switch msg.Method {
	case "initialize":
		asked := strAt(params(msg.Params), "protocolVersion")
		proto := protocol
		for _, s := range supported {
			if s == asked {
				proto = asked
				break
			}
		}
		ins := instructions
		if manager {
			ins = "This is a repo manager, not a repo. Call ask(question) for anything that needs reasoning over the codebase: a cheap model answers in a few lines and names the facts it used. Call plan(slug) for the next task and status() for health. No tool returns file contents — if you need code, read the file yourself with your own tools."
		} else if multi {
			ins = instructions + " This server serves several projects: pass `project` (a name from `weave projects`) to pick one; omit it for this repo."
		}
		reply(initializeResult{ProtocolVersion: proto, Capabilities: capabilities{}, ServerInfo: serverInfo{Name: "weave", Version: version.Version}, Instructions: ins})
	case "tools/list":
		reply(struct {
			Tools []tool `json:"tools"`
		}{visibleTools(multi, manager)})
	case "resources/list", "resources/templates/list":
		reply(struct {
			Resources         []any `json:"resources"`
			ResourceTemplates []any `json:"resourceTemplates"`
		}{[]any{}, []any{}})
	case "prompts/list":
		reply(struct {
			Prompts []any `json:"prompts"`
		}{[]any{}})
	case "logging/setLevel", "ping":
		reply(struct{}{})
	case "tools/call":
		started := time.Now()
		p := params(msg.Params)
		name := strAt(p, "name")
		args := objAt(p, "arguments")
		g, err := src(strAt(args, "project"))
		if err != nil {
			reply(toolResult{Content: []content{{Type: "text", Text: "weave error: " + err.Error()}}, IsError: true})
			return
		}
		text, rerr := callTool(name, args, g, invalidate)
		if rerr != nil {
			fail(rerr.Code, rerr.Message)
			return
		}
		runtime.calls++
		runtime.ms = append(runtime.ms, float64(time.Since(started).Milliseconds()))
		if len(runtime.ms) > 200 {
			runtime.ms = runtime.ms[1:]
		}
		tokens := int(math.Ceil(float64(len(text)) / 4))
		usage.Record(g.Root, name, tokens, float64(time.Since(started).Milliseconds()))
		reply(toolResult{Content: []content{{Type: "text", Text: text + "\n" + receipt(g, text)}}})
	default:
		method := msg.Method
		if method == "" {
			method = "undefined" // JSON-RPC with no method, as V8 printed it
		}
		fail(-32601, "method not found: "+method)
	}
}

func callTool(name string, a map[string]any, g *weave.Graph, invalidate func()) (string, *rpcError) {
	if name == "" {
		name = "undefined"
	}
	switch name {
	case "context":
		files := filesArg(a)
		if len(files) == 0 {
			return "", &rpcError{-32602, "context needs `files`: an array of repo-relative paths"}
		}
		depth := strAt(a, "depth")
		if depth != "code" {
			depth = "names"
		}
		return weave.Context(g, files, numberAt(a, "budget", 350), depth), nil
	case "impact":
		files := filesArg(a)
		if len(files) == 0 {
			return "", &rpcError{-32602, "impact needs `files`: an array of repo-relative paths"}
		}
		return weave.Impact(g, files), nil
	case "search":
		q := strAt(a, "query")
		if q == "" {
			return "", &rpcError{-32602, "search needs `query`"}
		}
		limit := intAt(a, "limit", 20)
		if !truthy(a["text"]) {
			return weave.Search(g, q, limit), nil
		}
		return grep.Grep(g, q, limit, truthy(a["regex"])), nil
	case "remember":
		files := filesArg(a)
		note := strAt(a, "note")
		if len(files) == 0 || note == "" {
			return "", &rpcError{-32602, "remember needs `files` and `note`"}
		}
		entry, err := weave.Remember(g.Root, files, note, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
		if err != nil || entry == nil {
			if weave.Derivable(note) {
				return "not stored: who owns a file is in CODEOWNERS and the map; keep notes for decisions and gotchas", nil
			}
			return "nothing new to remember", nil
		}
		return fmt.Sprintf("remembered for %d file(s): %s", len(entry.Files), entry.Note), nil
	case "recall":
		text, err := weave.RecallText(g.Root, filesArg(a))
		if err != nil {
			return "", &rpcError{-32602, err.Error()}
		}
		return text, nil
	case "changes":
		slug := strAt(a, "slug")
		if slug != "" {
			return weave.ChangeView(g, slug), nil
		}
		return weave.ListChanges(g, 25), nil
	case "task_done", "task_reopen":
		change, task := strAt(a, "change"), strAt(a, "task")
		if change == "" || task == "" {
			return "", &rpcError{-32602, name + " needs `change` and `task`; plan(slug) returns the task id"}
		}
		done := name == "task_done"
		dir, changed, err := weave.SetSpecTask(g.Root, change, task, done)
		if err != nil {
			return "", &rpcError{-32602, err.Error()}
		}
		if changed && invalidate != nil {
			invalidate()
		}
		state, verb := "open", "reopened"
		if done {
			state, verb = "done", "done"
		}
		if !changed {
			return dir + " " + task + " is already " + state, nil
		}
		return verb + " " + dir + " " + task, nil
	case "change_new":
		name, title := strAt(a, "name"), strAt(a, "title")
		if name == "" {
			return "", &rpcError{-32602, "change_new needs `name`, e.g. billing-retry"}
		}
		slug, err := weave.NewSpecChange(g.Root, name, title, time.Now())
		if err != nil {
			return "", &rpcError{-32602, err.Error()}
		}
		if invalidate != nil {
			invalidate()
		}
		return "created specs/changes/" + slug, nil
	case "change_delete":
		change := strAt(a, "change")
		if change == "" || !truthy(a["confirm"]) {
			return "", &rpcError{-32602, "change_delete needs `change` and `confirm: true`; it removes specs/changes/<change> and cannot be undone"}
		}
		name, err := weave.DeleteSpecChange(g.Root, change)
		if err != nil {
			return "", &rpcError{-32602, err.Error()}
		}
		if invalidate != nil {
			invalidate()
		}
		return "deleted specs/changes/" + name, nil
	case "plan":
		slug := strAt(a, "slug")
		if slug == "" {
			return "", &rpcError{-32602, "plan needs `slug`; call changes() for the list"}
		}
		return weave.Plan(g, slug, strAt(a, "task")), nil
	case "ask":
		q := strAt(a, "question")
		if q == "" {
			return "", &rpcError{-32602, "ask needs `question`"}
		}
		return weave.FormatAsk(weave.Ask(g, q, filesArg(a)), 18), nil
	case "changed":
		from := strAt(a, "from")
		ref := strAt(a, "ref")
		if ref == "" {
			ref = "HEAD"
		}
		name := ref
		var base *weave.Graph
		if from != "" {
			p := from
			if !filepath.IsAbs(p) {
				p = filepath.Join(g.Root, p)
			}
			base = baseline.GraphFromFile(p)
			name = from
		} else {
			base = baseline.GraphAt(g.Root, ref)
		}
		if base == nil {
			if from != "" {
				return "", &rpcError{-32602, "cannot read graph " + from}
			}
			return "", &rpcError{-32602, "cannot read " + ref}
		}
		return delta.Delta(g, base, name, truthy(a["all"])), nil
	case "verify":
		return verify.Run(g, g.Root, strAt(a, "against"), truthy(a["strict"]), filesArg(a)).Text(), nil
	case "status":
		return weave.Doctor(g) + "\n" + runtimeLine(g), nil
	default:
		names := make([]string, 0, len(toolDefs))
		for _, t := range toolDefs {
			names = append(names, t.Name)
		}
		return "", &rpcError{-32602, fmt.Sprintf("unknown tool %s; available: %s", name, strings.Join(names, ", "))}
	}
}

func receipt(g *weave.Graph, text string) string {
	tokens := int(math.Ceil(float64(len(text)) / 4))
	age := 0
	if t, err := time.Parse("2006-01-02T15:04:05.000Z", g.BuiltAt); err == nil {
		s := int(math.Round(time.Since(t).Seconds()))
		if s > 0 {
			age = s
		}
	}
	cov := "coverage unknown — re-index"
	if g.Coverage != nil {
		cov = fmt.Sprintf("coverage %d/%d route files · %d/%d calls",
			g.Coverage.Routes.Found, g.Coverage.Routes.Files, g.Coverage.Calls.Resolved, g.Coverage.Calls.Total)
	}
	return fmt.Sprintf("— %d tokens · graph %ds old · %s", tokens, age, cov)
}

func runtimeLine(g *weave.Graph) string {
	up := int(math.Round(time.Since(runtime.start).Seconds()))
	if runtime.calls == 0 {
		return fmt.Sprintf("runtime      up %ds · no tool calls yet", up)
	}
	sorted := append([]float64(nil), runtime.ms...)
	sortFloats(sorted)
	avg := 0.0
	for _, v := range runtime.ms {
		avg += v
	}
	avg /= float64(len(runtime.ms))
	idx := int(math.Floor(float64(len(sorted)) * 0.95))
	if idx > len(sorted)-1 {
		idx = len(sorted) - 1
	}
	lastIndex := ""
	if g.Perf != nil {
		lastIndex = fmt.Sprintf(" · last index %.0fms", g.Perf.TotalMs)
	}
	return fmt.Sprintf("runtime      up %ds · %d call(s) · avg %.1fms · p95 %.1fms%s", up, runtime.calls, avg, sorted[idx], lastIndex)
}

func sortFloats(v []float64) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// --- JS-shaped argument helpers -----------------------------------------

func params(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return map[string]any{}
	}
	return m
}

func objAt(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func strAt(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func numberAt(m map[string]any, key string, fallback float64) float64 {
	v, ok := m[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return math.NaN()
		}
		return f
	case bool:
		if n {
			return 1
		}
		return 0
	}
	return math.NaN()
}

func intAt(m map[string]any, key string, fallback int) int {
	n := numberAt(m, key, float64(fallback))
	if math.IsNaN(n) {
		return 0
	}
	return int(n)
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func filesArg(a map[string]any) []string {
	for _, key := range []string{"files", "file"} {
		v, ok := a[key]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case []any:
			out := make([]string, 0, len(x))
			for _, e := range x {
				if e == nil {
					continue
				}
				s := fmt.Sprint(e)
				if s != "" {
					out = append(out, s)
				}
			}
			return out
		case string:
			if x != "" {
				return []string{x}
			}
			return nil
		}
	}
	return nil
}
