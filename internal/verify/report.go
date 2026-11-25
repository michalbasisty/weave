package verify

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"sort"

	"github.com/michalbasisty/weave/internal/version"
)

// The gate detects a finding where it finds it and renders the same list three
// ways. Text and Markdown keep the sentences a reader already knows; JSON and
// SARIF read Findings directly, so no renderer has to parse a report back into
// data. A Finding is never derived from the rendered text: truncation in Text or
// Markdown ("… 4 more", one line per failure) must not cap what a machine sees.

// Severity is how a finding affects the run.
const (
	SeverityError       = "error"
	SeverityWarning     = "warning"
	SeverityOperational = "operational"
)

// Rules name the check that fired. They are stable identifiers: a script or a
// code-scanning setup may match on them, so rename one only with that in mind.
const (
	RuleRemovedEndpointCalled = "removed-endpoint-still-called"
	RuleRemovedNoCaller       = "removed-endpoint-no-caller"
	RuleOrphanCallSite        = "orphan-call-site"
	RuleSuspectRequirement    = "spec-suspect"
	RuleNoChangeRecord        = "no-change-record"
	RuleUntestedEndpoint      = "untested-endpoint"
	RuleStaleEvidence         = "stale-evidence"
	RuleRequirementTestFailed = "requirement-test-failing"
	RuleUnverifiedRequirement = "unverified-requirement"
	RuleMissingEvidence       = "missing-evidence"
	RuleOperational           = "operational"
)

var ruleDescriptions = map[string]string{
	RuleRemovedEndpointCalled: "A route the change removed is still called",
	RuleRemovedNoCaller:       "A route the change removed has no live caller",
	RuleOrphanCallSite:        "A call site in a changed file matches no endpoint",
	RuleSuspectRequirement:    "Requirement text changed without a code re-check",
	RuleNoChangeRecord:        "No spec change record names the changed files",
	RuleUntestedEndpoint:      "A touched endpoint has no direct test",
	RuleStaleEvidence:         "The JUnit report predates the changed source",
	RuleRequirementTestFailed: "A requirement's test is failing",
	RuleUnverifiedRequirement: "A requirement is implemented but never tested",
	RuleMissingEvidence:       "Requirements are linked but no JUnit report exists",
	RuleOperational:           "The gate could not read its input",
}

// Finding is one structured result of the gate.
type Finding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	// File and Line locate the finding when the gate knows where it is. For a
	// removed endpoint still called this is the caller, not the deleted route.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// Endpoint is the route the finding names, when it names one.
	Endpoint string `json:"endpoint,omitempty"`
	// Caller is the call site that still names a removed endpoint.
	Caller string `json:"caller,omitempty"`
	// Requirement is the requirement id a finding is about, when it has one.
	Requirement string `json:"requirement,omitempty"`
}

// Status is the run's verdict in one word. Operational outranks a contract
// failure: a gate that could not read its input has no verdict to give.
func (r Result) Status() string {
	switch {
	case len(r.Operational) > 0:
		return "operational"
	case len(r.Errors) > 0:
		return "fail"
	default:
		return "pass"
	}
}

// OperationalResult is a run the gate could not perform. It carries no verdict;
// Code() returns 2 and every renderer says the gate could not run.
func OperationalResult(scope, message string) Result {
	return Result{
		Scope:       scope,
		Operational: []string{message},
		Findings: []Finding{{
			Rule:     RuleOperational,
			Severity: SeverityOperational,
			Message:  message,
		}},
	}
}

type jsonReport struct {
	Scope     string    `json:"scope"`
	Status    string    `json:"status"`
	ExitCode  int       `json:"exitCode"`
	Findings  []Finding `json:"findings"`
	Changes   []string  `json:"changes,omitempty"`
	Pages     []string  `json:"pages,omitempty"`
	Endpoints []string  `json:"endpoints,omitempty"`
	Commands  []string  `json:"commands,omitempty"`
	Broken    []Broken  `json:"broken,omitempty"`
}

// JSON renders the result for a script. The document is stable and complete:
// status and exitCode state the verdict, findings carry every detection with
// its location, and the descriptive lists repeat what Text and Markdown show.
func (r Result) JSON() ([]byte, error) {
	findings := r.Findings
	if findings == nil {
		findings = []Finding{}
	}
	return json.MarshalIndent(jsonReport{
		Scope:     r.Scope,
		Status:    r.Status(),
		ExitCode:  r.Code(),
		Findings:  findings,
		Changes:   r.Changes,
		Pages:     r.Pages,
		Endpoints: r.Endpoints,
		Commands:  r.Commands,
		Broken:    r.Broken,
	}, "", "  ")
}

// --- SARIF 2.1.0 ---------------------------------------------------------

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool         `json:"tool"`
	Invocations []sarifInvocation `json:"invocations"`
	Results     []sarifResult     `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version,omitempty"`
	Rules          []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID               string       `json:"id"`
	ShortDescription sarifMessage `json:"shortDescription"`
}

type sarifInvocation struct {
	ExecutionSuccessful        bool                `json:"executionSuccessful"`
	ToolExecutionNotifications []sarifNotification `json:"toolExecutionNotifications,omitempty"`
}

type sarifNotification struct {
	Level   string       `json:"level"`
	Message sarifMessage `json:"message"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

// SARIF renders the result for GitHub code scanning and other SARIF readers.
// Contract failures become results; a gate that could not run reports through
// the invocation (executionSuccessful false and a notification), so an input
// it could not read never looks like a finding in the changed code.
func (r Result) SARIF() ([]byte, error) {
	results := make([]sarifResult, 0, len(r.Findings))
	rules := map[string]bool{}
	for _, f := range r.Findings {
		if f.Severity == SeverityOperational {
			continue
		}
		level := f.Severity
		if level != SeverityError && level != SeverityWarning {
			level = "note"
		}
		res := sarifResult{
			RuleID:  f.Rule,
			Level:   level,
			Message: sarifMessage{Text: f.Message},
		}
		if f.File != "" {
			phys := sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{URI: artifactURI(f.File)}}
			if f.Line > 0 {
				phys.Region = &sarifRegion{StartLine: f.Line}
			}
			res.Locations = []sarifLocation{{PhysicalLocation: phys}}
		}
		results = append(results, res)
		rules[f.Rule] = true
	}

	var ruleList []sarifRule
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ruleList = append(ruleList, sarifRule{
			ID:               id,
			ShortDescription: sarifMessage{Text: ruleDescriptions[id]},
		})
	}

	inv := sarifInvocation{ExecutionSuccessful: len(r.Operational) == 0}
	for _, msg := range r.Operational {
		inv.ToolExecutionNotifications = append(inv.ToolExecutionNotifications, sarifNotification{
			Level:   "error",
			Message: sarifMessage{Text: msg},
		})
	}

	return json.MarshalIndent(sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "weave",
				InformationURI: "https://github.com/michalbasisty/weave",
				Version:        version.Version,
				Rules:          ruleList,
			}},
			Invocations: []sarifInvocation{inv},
			Results:     results,
		}},
	}, "", "  ")
}

// artifactURI makes a graph path a valid relative URI: backslashes become
// slashes and anything that would end a path or start a query is escaped.
func artifactURI(file string) string {
	return (&url.URL{Path: filepath.ToSlash(file)}).String()
}
