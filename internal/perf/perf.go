// Package perf is the Go port of src/perf.ts: timings for every phase of a run,
// appended to .weave/perf.jsonl so growth and regressions are visible.
package perf

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

// Record is one index run. PhaseOrder is the order the phases were written in:
// JS objects keep insertion order, and Report prints the newest run's keys.
type Record struct {
	At         string             `json:"at"`
	Files      float64            `json:"files"`
	Nodes      float64            `json:"nodes"`
	Edges      float64            `json:"edges"`
	TotalMs    float64            `json:"totalMs"`
	Phases     map[string]float64 `json:"phases"`
	HeapMb     float64            `json:"heapMb"`
	PhaseOrder []string           `json:"-"`
}

// Timer records the time between laps, rounded the way Math.round(n*10)/10 does.
type Timer struct {
	marks map[string]float64
	order []string
	last  time.Time
	start time.Time
}

// NewTimer starts a timer now.
func NewTimer() *Timer {
	now := time.Now()
	return &Timer{marks: map[string]float64{}, last: now, start: now}
}

// Lap records the time since the previous lap under name and returns it.
func (t *Timer) Lap(name string) float64 {
	if t.start.IsZero() {
		now := time.Now()
		t.start, t.last = now, now
	}
	now := time.Now()
	if t.marks == nil {
		t.marks = map[string]float64{}
	}
	if _, ok := t.marks[name]; !ok {
		t.order = append(t.order, name)
	}
	t.marks[name] = round(now.Sub(t.last).Seconds() * 1000)
	t.last = now
	return t.marks[name]
}

// Total is the time since the timer started, rounded like Lap.
func (t *Timer) Total() float64 {
	if t.start.IsZero() {
		return 0
	}
	return round(time.Since(t.start).Seconds() * 1000)
}

// Phases returns the laps by name.
func (t *Timer) Phases() map[string]float64 { return t.marks }

// PhaseOrder returns the lap names in insertion order.
func (t *Timer) PhaseOrder() []string { return append([]string(nil), t.order...) }

func round(n float64) float64 { return math.Floor(n*10+0.5) / 10 }

// jsRound is Math.round: halves go toward +Infinity.
func jsRound(n float64) int { return int(math.Floor(n + 0.5)) }

func perfPath(root string) string { return filepath.Join(root, ".weave", "perf.jsonl") }

// Append adds one record, rotating the file once it passes 400 kB. Write errors
// are ignored: a read-only checkout must not fail an index run.
func Append(root string, record Record) {
	p := perfPath(root)
	if fi, err := os.Stat(p); err == nil && fi.Size() > 400_000 {
		_ = os.WriteFile(p, nil, 0o644)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(encodeRecord(record), '\n'))
	_ = f.Close()
}

// Read returns every parseable line; a missing or unreadable file reads as none.
func Read(root string) []Record {
	raw, err := os.ReadFile(perfPath(root))
	if err != nil {
		return []Record{}
	}
	out := make([]Record, 0)
	for _, line := range strings.Split(jsstr.DecodeUTF8(raw), "\n") {
		if line == "" {
			continue
		}
		if record, ok := decodeRecord(line); ok {
			out = append(out, record)
		}
	}
	return out
}

// Report is the `weave perf` table. window defaults to 8 when not positive.
func Report(records []Record, window int) string {
	if len(records) == 0 {
		return "no runs recorded yet — run: weave index"
	}
	if window <= 0 {
		window = 8
	}
	rows := records
	if len(rows) > window {
		rows = rows[len(rows)-window:]
	}
	phases := rows[len(rows)-1].phaseKeys()
	header := []string{jsstr.PadEnd("when", 20), jsstr.PadStart("files", 6), jsstr.PadStart("nodes", 6), jsstr.PadStart("edges", 6), jsstr.PadStart("total", 8)}
	for _, p := range phases {
		header = append(header, jsstr.PadStart(p, 9))
	}
	out := []string{"last " + strconv.Itoa(len(rows)) + " index run(s)", strings.Join(header, " ")}
	for _, r := range rows {
		when := strings.Replace(jsstr.Slice(r.At, 0, 19), "T", " ", 1)
		cells := []string{
			jsstr.PadEnd(when, 20),
			jsstr.PadStart(jsjson.FormatNumber(r.Files), 6),
			jsstr.PadStart(jsjson.FormatNumber(r.Nodes), 6),
			jsstr.PadStart(jsjson.FormatNumber(r.Edges), 6),
			jsstr.PadStart(jsjson.FormatNumber(r.TotalMs)+"ms", 8),
		}
		for _, p := range phases {
			cells = append(cells, jsstr.PadStart(jsjson.FormatNumber(r.Phases[p])+"ms", 9))
		}
		out = append(out, strings.Join(cells, " "))
	}
	totals := make([]float64, len(rows))
	for i, r := range rows {
		totals[i] = r.TotalMs
	}
	sum := 0.0
	max := totals[0]
	for _, n := range totals {
		sum += n
		if n > max {
			max = n
		}
	}
	avg := sum / float64(len(totals))
	first, last := totals[0], totals[len(totals)-1]
	trend := 0
	if first > 0 {
		trend = jsRound(((last - first) / first) * 100)
	}
	sign := ""
	if trend >= 0 {
		sign = "+"
	}
	out = append(out, "")
	out = append(out, "avg "+strconv.Itoa(jsRound(avg))+"ms · max "+strconv.Itoa(jsRound(max))+"ms · heap "+jsjson.FormatNumber(rows[len(rows)-1].HeapMb)+"MB · trend "+sign+strconv.Itoa(trend)+"% over the window")
	return strings.Join(out, "\n")
}

// phaseKeys is Object.keys(phases): the recorded order, then any key the record
// added without one, sorted so the output stays stable.
func (r Record) phaseKeys() []string {
	seen := make(map[string]bool, len(r.Phases))
	out := make([]string, 0, len(r.Phases))
	for _, k := range r.PhaseOrder {
		if _, ok := r.Phases[k]; ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	rest := make([]string, 0, len(r.Phases)-len(out))
	for k := range r.Phases {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// encodeRecord is JSON.stringify(record): same key order, compact.
func encodeRecord(r Record) []byte {
	phases := &jsjson.Object{}
	for _, k := range r.phaseKeys() {
		phases.Set(k, jsjson.Num(r.Phases[k]))
	}
	o := &jsjson.Object{}
	o.Set("at", jsjson.Str(r.At))
	o.Set("files", jsjson.Num(r.Files))
	o.Set("nodes", jsjson.Num(r.Nodes))
	o.Set("edges", jsjson.Num(r.Edges))
	o.Set("totalMs", jsjson.Num(r.TotalMs))
	o.Set("phases", jsjson.Obj(phases))
	o.Set("heapMb", jsjson.Num(r.HeapMb))
	return jsjson.Marshal(jsjson.Obj(o))
}

func decodeRecord(line string) (Record, bool) {
	v, err := jsjson.Parse([]byte(line))
	if err != nil {
		return Record{}, false
	}
	o := v.Object()
	if o == nil {
		return Record{}, false
	}
	r := Record{Phases: map[string]float64{}}
	r.At = fieldStr(o, "at")
	r.Files = fieldNum(o, "files")
	r.Nodes = fieldNum(o, "nodes")
	r.Edges = fieldNum(o, "edges")
	r.TotalMs = fieldNum(o, "totalMs")
	r.HeapMb = fieldNum(o, "heapMb")
	if pv, ok := o.Get("phases"); ok {
		if po := pv.Object(); po != nil {
			for _, k := range po.Keys() {
				p, _ := po.Get(k)
				r.Phases[k] = p.Float()
				r.PhaseOrder = append(r.PhaseOrder, k)
			}
		}
	}
	return r, true
}

func fieldStr(o *jsjson.Object, key string) string {
	v, ok := o.Get(key)
	if !ok {
		return ""
	}
	return v.Str()
}

func fieldNum(o *jsjson.Object, key string) float64 {
	v, ok := o.Get(key)
	if !ok {
		return 0
	}
	return v.Float()
}
