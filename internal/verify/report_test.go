package verify

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michalbasisty/weave/internal/weave"
)

// brokenGraph is one removed endpoint with one live caller, the shape a rename
// leaves behind. Its location is what a machine reader needs.
func brokenGraph(root string) *weave.Graph {
	file, line := "packages/web/src/orders.tsx", 12
	return &weave.Graph{
		Root: root,
		Nodes: []*weave.GNode{
			{ID: "call1", Kind: weave.KindCall, Name: "GET /api/v1/users/42", File: &file, Line: &line},
		},
		PrevEndpoints: []string{"GET /api/v1/users/:id"},
	}
}

type jsonCheck struct {
	Scope    string    `json:"scope"`
	Status   string    `json:"status"`
	ExitCode int       `json:"exitCode"`
	Findings []Finding `json:"findings"`
	Commands []string  `json:"commands"`
}

func decodeJSON(t *testing.T, raw []byte) jsonCheck {
	t.Helper()
	var got jsonCheck
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	return got
}

// A failure reaches JSON with the caller's file and line, so a script never has
// to parse "from packages/web/src/orders.tsx:12" out of the message.
func TestJSONFailureCarriesCallerLocation(t *testing.T) {
	res := Run(brokenGraph(t.TempDir()), t.TempDir(), "", false, nil)
	raw, err := res.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON(t, raw)
	if got.Status != "fail" || got.ExitCode != 1 {
		t.Fatalf("status %q exit %d:\n%s", got.Status, got.ExitCode, raw)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("findings = %+v", got.Findings)
	}
	f := got.Findings[0]
	if f.Rule != RuleRemovedEndpointCalled || f.Severity != SeverityError {
		t.Fatalf("finding = %+v", f)
	}
	if f.File != "packages/web/src/orders.tsx" || f.Line != 12 {
		t.Fatalf("location = %s:%d", f.File, f.Line)
	}
	if f.Endpoint != "GET /api/v1/users/:id" || f.Caller != "packages/web/src/orders.tsx:12" {
		t.Fatalf("endpoint %q caller %q", f.Endpoint, f.Caller)
	}
}

// A pass is still a complete document: findings is an empty array, not null,
// so a consumer can iterate without a special case.
func TestJSONPassIsAnEmptyFindingList(t *testing.T) {
	root := t.TempDir()
	res := Run(&weave.Graph{Root: root}, root, "", false, nil)
	if res.Code() != 0 {
		t.Fatalf("code = %d:\n%s", res.Code(), res.Text())
	}
	raw, err := res.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON(t, raw)
	if got.Status != "pass" || got.ExitCode != 0 {
		t.Fatalf("status %q exit %d:\n%s", got.Status, got.ExitCode, raw)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("pass carried findings: %+v", got.Findings)
	}
	if strings.Contains(string(raw), `"findings": null`) {
		t.Fatalf("findings must be an array:\n%s", raw)
	}
}

// A run that could not read its input has no verdict: JSON says operational and
// the exit code stays 2, never 0 or 1.
func TestJSONOperationalIsNotAPassOrFailure(t *testing.T) {
	root := t.TempDir()
	res := Run(&weave.Graph{Root: root}, root, "no-such-ref", false, []string{"a.ts"})
	raw, err := res.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON(t, raw)
	if got.Status != "operational" || got.ExitCode != 2 {
		t.Fatalf("status %q exit %d:\n%s", got.Status, got.ExitCode, raw)
	}
	if len(got.Findings) == 0 || got.Findings[0].Severity != SeverityOperational || got.Findings[0].Rule != RuleOperational {
		t.Fatalf("operational finding = %+v", got.Findings)
	}
}

type sarifDoc struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

func decodeSARIF(t *testing.T, raw []byte) sarifDoc {
	t.Helper()
	var doc sarifDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("invalid SARIF: %v\n%s", err, raw)
	}
	return doc
}

// SARIF 2.1.0 names the rule, the level and the caller, and encodes the path as
// a URI so a reader can resolve it.
func TestSARIFFailureCarriesRuleAndEncodedURI(t *testing.T) {
	root := t.TempDir()
	file, line := "packages/web/src/orders list.tsx", 12
	g := &weave.Graph{
		Root: root,
		Nodes: []*weave.GNode{
			{ID: "call1", Kind: weave.KindCall, Name: "GET /api/v1/users/42", File: &file, Line: &line},
		},
		PrevEndpoints: []string{"GET /api/v1/users/:id"},
	}
	raw, err := Run(g, root, "", false, nil).SARIF()
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeSARIF(t, raw)
	if doc.Version != "2.1.0" || !strings.Contains(doc.Schema, "sarif-2.1.0") {
		t.Fatalf("header = %+v", doc)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d", len(doc.Runs))
	}
	run := doc.Runs[0]
	if run.Tool.Driver.Name != "weave" || len(run.Tool.Driver.Rules) != 1 || run.Tool.Driver.Rules[0].ID != RuleRemovedEndpointCalled {
		t.Fatalf("driver = %+v", run.Tool.Driver)
	}
	if len(run.Invocations) != 1 || !run.Invocations[0].ExecutionSuccessful {
		t.Fatalf("invocation = %+v", run.Invocations)
	}
	if len(run.Results) != 1 {
		t.Fatalf("results = %+v", run.Results)
	}
	res := run.Results[0]
	if res.RuleID != RuleRemovedEndpointCalled || res.Level != "error" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Locations) != 1 {
		t.Fatalf("locations = %+v", res.Locations)
	}
	loc := res.Locations[0].PhysicalLocation
	if loc.ArtifactLocation.URI != "packages/web/src/orders%20list.tsx" {
		t.Fatalf("uri = %q", loc.ArtifactLocation.URI)
	}
	if loc.Region == nil || loc.Region.StartLine != 12 {
		t.Fatalf("region = %+v", loc.Region)
	}
}

// A clean run is a successful invocation with no results: code scanning shows
// nothing, which is exactly the pass.
func TestSARIFPassHasNoResults(t *testing.T) {
	root := t.TempDir()
	res := Run(&weave.Graph{Root: root}, root, "", false, nil)
	if res.Code() != 0 {
		t.Fatalf("code = %d:\n%s", res.Code(), res.Text())
	}
	raw, err := res.SARIF()
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeSARIF(t, raw)
	srun := doc.Runs[0]
	if len(srun.Results) != 0 {
		t.Fatalf("pass carried results: %+v", srun.Results)
	}
	if len(srun.Invocations) != 1 || !srun.Invocations[0].ExecutionSuccessful || len(srun.Invocations[0].ToolExecutionNotifications) != 0 {
		t.Fatalf("invocation = %+v", srun.Invocations)
	}
}

// Operational failures live in the invocation, not in results: an input the
// gate could not read must not look like a defect in the changed code.
func TestSARIFOperationalUsesTheInvocation(t *testing.T) {
	root := t.TempDir()
	res := Run(&weave.Graph{Root: root}, root, "--main", false, []string{"a.ts"})
	if res.Code() != 2 {
		t.Fatalf("code = %d", res.Code())
	}
	raw, err := res.SARIF()
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeSARIF(t, raw)
	run := doc.Runs[0]
	if len(run.Results) != 0 {
		t.Fatalf("operational run carried results: %+v", run.Results)
	}
	if len(run.Invocations) != 1 || run.Invocations[0].ExecutionSuccessful {
		t.Fatalf("invocation = %+v", run.Invocations)
	}
	notes := run.Invocations[0].ToolExecutionNotifications
	if len(notes) != 1 || notes[0].Level != "error" || !strings.Contains(notes[0].Message.Text, "not a usable git ref") {
		t.Fatalf("notifications = %+v", notes)
	}
}

// strict is the only switch, and it must survive into the structured report.
func TestStrictRaisesUntestedToAnErrorFinding(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixture")
	files := []string{"packages/backend/src/modules/orders/orders.routes.ts"}

	untyped := Run(weave.BuildGraph(root), root, "", false, files)
	strict := Run(weave.BuildGraph(root), root, "", true, files)
	find := func(res Result) *Finding {
		for i := range res.Findings {
			if res.Findings[i].Rule == RuleUntestedEndpoint {
				return &res.Findings[i]
			}
		}
		return nil
	}
	if f := find(untyped); f == nil || f.Severity != SeverityWarning {
		t.Fatalf("lenient finding = %+v", f)
	}
	f := find(strict)
	if f == nil || f.Severity != SeverityError {
		t.Fatalf("strict finding = %+v", f)
	}

	raw, err := strict.SARIF()
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeSARIF(t, raw)
	found := false
	for _, r := range doc.Runs[0].Results {
		if r.RuleID == RuleUntestedEndpoint && r.Level == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("strict SARIF did not raise untested to error: %+v", doc.Runs[0].Results)
	}
}
