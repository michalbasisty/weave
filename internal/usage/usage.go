// Package usage is the Go port of src/usage.ts: what weave actually served, so
// the saving is measured instead of claimed.
package usage

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/michalbasisty/weave/internal/jsjson"
	"github.com/michalbasisty/weave/internal/jsstr"
)

// Entry is one JSON line of the ledger.
type Entry struct {
	At     string
	Tool   string
	Tokens float64
	Ms     float64
}

// Path is <root>/.weave/usage.jsonl.
func Path(root string) string { return filepath.Join(root, ".weave", "usage.jsonl") }

// Record appends one tool call to the ledger. A read-only checkout must not fail
// a tool call, so write errors are swallowed; WEAVE_NO_USAGE=1 disables it.
func Record(root, tool string, tokens int, ms float64) {
	if os.Getenv("WEAVE_NO_USAGE") == "1" {
		return
	}
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		return
	}
	o := &jsjson.Object{}
	o.Set("at", jsjson.Str(time.Now().UTC().Format("2006-01-02T15:04:05.000Z")))
	o.Set("tool", jsjson.Str(tool))
	o.Set("tokens", jsjson.Int(tokens))
	o.Set("ms", jsjson.Num(jsRound(ms)))
	line := append(jsjson.Marshal(jsjson.Obj(o)), '\n')

	f, err := os.OpenFile(Path(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(line)
	_ = f.Close()
}

// Read returns the last 5000 ledger entries, skipping torn lines.
func Read(root string) []Entry {
	raw, err := os.ReadFile(Path(root))
	if err != nil {
		return []Entry{}
	}
	lines := make([]string, 0, 64)
	for _, line := range strings.Split(jsstr.DecodeUTF8(raw), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 5000 {
		lines = lines[len(lines)-5000:]
	}

	out := make([]Entry, 0, len(lines))
	for _, line := range lines {
		v, err := jsjson.Parse([]byte(line))
		if err != nil {
			continue
		}
		o := v.Object()
		if o == nil {
			continue
		}
		tool, ok := o.Get("tool")
		if !ok || tool.Kind() != jsjson.KindString {
			continue
		}
		tokens, ok := o.Get("tokens")
		if !ok || tokens.Kind() != jsjson.KindNumber {
			continue
		}
		e := Entry{Tool: tool.Str(), Tokens: tokens.Float()}
		if at, ok := o.Get("at"); ok && at.Kind() == jsjson.KindString {
			e.At = at.Str()
		}
		if ms, ok := o.Get("ms"); ok && ms.Kind() == jsjson.KindNumber {
			e.Ms = ms.Float()
		}
		out = append(out, e)
	}
	return out
}

// Gain summarises the calls recorded in the last days days.
func Gain(root string, days int) string {
	window := days
	if window < 1 {
		window = 1
	}
	cutoff := time.Now().UnixMilli() - int64(window)*86400000
	rows := make([]Entry, 0, 64)
	for _, u := range Read(root) {
		if at, ok := parseAt(u.At); ok && at >= cutoff {
			rows = append(rows, u)
		}
	}
	if len(rows) == 0 {
		return "gain: no usage recorded in the last " + strconv.Itoa(days) + " day(s)\n" +
			"  MCP tool calls append to " + Path(root) + "; run an agent against this repo, then retry."
	}

	tokens := make([]float64, len(rows))
	for i, u := range rows {
		tokens[i] = u.Tokens
	}
	sort.Float64s(tokens)
	sum := 0.0
	for _, t := range tokens {
		sum += t
	}
	p := func(q float64) float64 {
		i := int(math.Floor(float64(len(tokens)) * q))
		if i > len(tokens)-1 {
			i = len(tokens) - 1
		}
		if i < 0 {
			i = 0
		}
		return tokens[i]
	}

	type stat struct {
		name   string
		n      int
		tokens float64
	}
	var tools []*stat
	by := map[string]*stat{}
	for _, u := range rows {
		e := by[u.Tool]
		if e == nil {
			e = &stat{name: u.Tool}
			by[u.Tool] = e
			tools = append(tools, e)
		}
		e.n++
		e.tokens += u.Tokens
	}
	sort.SliceStable(tools, func(i, j int) bool { return tools[i].tokens > tools[j].tokens })
	parts := make([]string, len(tools))
	for i, e := range tools {
		parts[i] = e.name + " " + strconv.Itoa(e.n) + " (" + localeNumber(e.tokens) + ")"
	}

	return "gain: " + strconv.Itoa(len(rows)) + " call(s) in the last " + strconv.Itoa(days) + " day(s)\n" +
		"tokens      total " + localeNumber(sum) + " · median " + jsjson.FormatNumber(p(0.5)) + " · p95 " + jsjson.FormatNumber(p(0.95)) + "\n" +
		"by tool     " + strings.Join(parts, " · ") + "\n" +
		"ledger      " + Path(root)
}

// parseAt is Date.parse for the timestamps the ledger holds.
func parseAt(s string) (int64, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli(), true
		}
	}
	return 0, false
}

// jsRound is Math.round: half toward +Infinity, not away from zero.
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// localeNumber is Number.prototype.toLocaleString with Node's default en-US
// formatting: thousands separators, at most 3 fraction digits.
func localeNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return groupThousands(strconv.FormatFloat(f, 'f', 0, 64))
	}
	s := strings.TrimRight(strconv.FormatFloat(f, 'f', 3, 64), "0")
	s = strings.TrimSuffix(s, ".")
	whole, frac, ok := strings.Cut(s, ".")
	if !ok {
		return groupThousands(whole)
	}
	return groupThousands(whole) + "." + frac
}

func groupThousands(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}
