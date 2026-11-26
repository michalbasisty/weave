package perf

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Produced by running src/perf.ts via node (Node 24.16.0) on the same records.
const goldenWindow8 = "last 3 index run(s)\n" +
	"when                  files  nodes  edges    total      scan     parse     links\n" +
	"2026-01-01 10:00:00      12     30     40  123.4ms    20.5ms    80.9ms      22ms\n" +
	"2026-01-02 11:30:00      15     35     48  150.2ms    21.2ms    90.1ms    39.9ms\n" +
	"2026-01-03 12:45:00      20     50     70    201ms      30ms     120ms      51ms\n" +
	"\n" +
	"avg 158ms · max 201ms · heap 18.1MB · trend +63% over the window"

const goldenWindow2 = "last 2 index run(s)\n" +
	"when                  files  nodes  edges    total      scan     parse     links\n" +
	"2026-01-02 11:30:00      15     35     48  150.2ms    21.2ms    90.1ms    39.9ms\n" +
	"2026-01-03 12:45:00      20     50     70    201ms      30ms     120ms      51ms\n" +
	"\n" +
	"avg 176ms · max 201ms · heap 18.1MB · trend +34% over the window"

func sampleRecords() []Record {
	return []Record{
		{At: "2026-01-01T10:00:00.000Z", Files: 12, Nodes: 30, Edges: 40, TotalMs: 123.4, Phases: map[string]float64{"scan": 20.5, "parse": 80.9, "links": 22}, PhaseOrder: []string{"scan", "parse", "links"}, HeapMb: 12.5},
		{At: "2026-01-02T11:30:00.123Z", Files: 15, Nodes: 35, Edges: 48, TotalMs: 150.2, Phases: map[string]float64{"scan": 21.2, "parse": 90.1, "links": 39.9}, PhaseOrder: []string{"scan", "parse", "links"}, HeapMb: 14.2},
		{At: "2026-01-03T12:45:00.000Z", Files: 20, Nodes: 50, Edges: 70, TotalMs: 201, Phases: map[string]float64{"scan": 30, "parse": 120, "links": 51}, PhaseOrder: []string{"scan", "parse", "links"}, HeapMb: 18.1},
	}
}

func TestReportMatchesTypeScript(t *testing.T) {
	records := sampleRecords()
	if got := Report(records, 8); got != goldenWindow8 {
		t.Errorf("window 8:\ngot  %q\nwant %q", got, goldenWindow8)
	}
	if got := Report(records, 2); got != goldenWindow2 {
		t.Errorf("window 2:\ngot  %q\nwant %q", got, goldenWindow2)
	}
	if got, want := Report(nil, 8), "no runs recorded yet — run: weave index"; got != want {
		t.Errorf("empty: got %q, want %q", got, want)
	}
}

func TestTimerLapOrderAndRounding(t *testing.T) {
	timer := NewTimer()
	first := timer.Lap("scan")
	if first < 0 {
		t.Fatalf("lap returned %v", first)
	}
	timer.Lap("parse")
	timer.Lap("scan")
	if got, want := strings.Join(timer.PhaseOrder(), ","), "scan,parse"; got != want {
		t.Errorf("phase order %q, want %q", got, want)
	}
	if len(timer.Phases()) != 2 {
		t.Errorf("phases %v", timer.Phases())
	}
	if timer.Total() < 0 {
		t.Errorf("total %v", timer.Total())
	}
	for _, c := range []struct{ in, want float64 }{{1.25, 1.3}, {2.5, 2.5}, {-1.54, -1.5}, {0, 0}} {
		if got := round(c.in); got != c.want {
			t.Errorf("round(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	if got := jsRound(2.5); got != 3 {
		t.Errorf("jsRound(2.5) = %d, want 3", got)
	}
	if got := jsRound(-0.5); got != 0 {
		t.Errorf("jsRound(-0.5) = %d, want 0", got)
	}
}

func TestAppendReadAndRotation(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := sampleRecords()[0]
	Append(root, record)
	Append(root, sampleRecords()[1])

	path := perfPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantLine := `{"at":"2026-01-01T10:00:00.000Z","files":12,"nodes":30,"edges":40,"totalMs":123.4,"phases":{"scan":20.5,"parse":80.9,"links":22},"heapMb":12.5}` + "\n"
	if got := string(raw); !strings.HasPrefix(got, wantLine) {
		t.Errorf("append wrote %q, want prefix %q", got, wantLine)
	}

	got := Read(root)
	if len(got) != 2 {
		t.Fatalf("read %d records, want 2", len(got))
	}
	if got[0].At != record.At || got[0].TotalMs != record.TotalMs || got[0].Phases["links"] != 22 {
		t.Errorf("round trip: %+v", got[0])
	}
	if strings.Join(got[0].PhaseOrder, ",") != "scan,parse,links" {
		t.Errorf("phase order %v", got[0].PhaseOrder)
	}

	// Over the cap, the next append starts a fresh file.
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 400_001), 0o644); err != nil {
		t.Fatal(err)
	}
	Append(root, sampleRecords()[2])
	got = Read(root)
	if len(got) != 1 || got[0].At != "2026-01-03T12:45:00.000Z" {
		t.Fatalf("after rotation read %+v", got)
	}
}

func TestReadToleratesMissingAndBadLines(t *testing.T) {
	root := t.TempDir()
	if got := Read(root); len(got) != 0 {
		t.Fatalf("missing file: %+v", got)
	}
	dir := filepath.Join(root, ".weave")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "not json\n\n" + `{"at":"2026-01-01T00:00:00.000Z","files":1,"nodes":2,"edges":3,"totalMs":4,"phases":{},"heapMb":5}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "perf.jsonl"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Read(root)
	if len(got) != 1 || got[0].Files != 1 || got[0].HeapMb != 5 {
		t.Fatalf("read %+v", got)
	}

	// No .weave directory: append quietly does nothing.
	missing := t.TempDir()
	Append(missing, sampleRecords()[0])
	if _, err := os.Stat(perfPath(missing)); !os.IsNotExist(err) {
		t.Errorf("append created a file without .weave: %v", err)
	}
	if _, err := os.Stat(filepath.Join(missing, ".weave")); !os.IsNotExist(err) {
		t.Errorf("append created .weave: %v", err)
	}
}

// TestAppendDoesNotFollowSymlinks pins the write to the repository root: a link
// planted at perf.jsonl or at .weave must not receive the record.
func TestAppendDoesNotFollowSymlinks(t *testing.T) {
	t.Run("file link", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		target := filepath.Join(outside, "perf.jsonl")
		if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, ".weave", "perf.jsonl")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		Append(root, sampleRecords()[0])
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != "keep\n" {
			t.Fatalf("append followed a file link: wrote %q", raw)
		}
	})

	t.Run("directory link", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, ".weave")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		Append(root, sampleRecords()[0])
		if _, err := os.Lstat(filepath.Join(outside, "perf.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("append wrote through a .weave link: %v", err)
		}
	})
}

// TestAppendKeepsExistingFileMode checks the append opens without changing the
// mode of a file that already exists.
func TestAppendKeepsExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(perfPath(root), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	Append(root, sampleRecords()[0])
	info, err := os.Stat(perfPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode after append = %o, want 600", perm)
	}
}

// TestAppendKeepsExistingLines checks the record is appended after the current
// end, not written over it.
func TestAppendKeepsExistingLines(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(perfPath(root), []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	Append(root, sampleRecords()[0])
	raw, err := os.ReadFile(perfPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "existing\n") {
		t.Fatalf("append overwrote existing content: %q", raw)
	}
}

// TestConcurrentAppendsKeepWholeLines relies on the O_APPEND write: each record
// is appended atomically, so no two writers overwrite one another's line.
func TestConcurrentAppendsKeepWholeLines(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := sampleRecords()[0]
	const writers = 32
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Append(root, record)
		}()
	}
	wg.Wait()
	if got := Read(root); len(got) != writers {
		t.Fatalf("read %d records, want %d", len(got), writers)
	}
}

// TestReadDoesNotFollowSymlinks pins the read to the repository root: a link
// planted at perf.jsonl or at .weave must not be read.
func TestReadDoesNotFollowSymlinks(t *testing.T) {
	t.Run("file link", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".weave"), 0o755); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		target := filepath.Join(outside, "perf.jsonl")
		line := `{"at":"2026-01-01T00:00:00.000Z","files":1,"nodes":2,"edges":3,"totalMs":4,"phases":{},"heapMb":5}` + "\n"
		if err := os.WriteFile(target, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, ".weave", "perf.jsonl")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		if got := Read(root); len(got) != 0 {
			t.Fatalf("read followed a file link: %+v", got)
		}
	})

	t.Run("directory link", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		line := `{"at":"2026-01-01T00:00:00.000Z","files":1,"nodes":2,"edges":3,"totalMs":4,"phases":{},"heapMb":5}` + "\n"
		if err := os.WriteFile(filepath.Join(outside, "perf.jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, ".weave")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		if got := Read(root); len(got) != 0 {
			t.Fatalf("read followed a .weave link: %+v", got)
		}
	})
}
