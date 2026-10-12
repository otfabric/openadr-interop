package report

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/openadr-interop/conformance/internal/probe"
	"github.com/otfabric/openadr-interop/conformance/internal/suite"
	"github.com/otfabric/openadr-interop/conformance/spec"
)

func sample() Run {
	results := []suite.Result{
		{ID: "PROGRAM-001", Title: "creates | a program", Refs: []string{"openadr3.yaml#createProgram"}, Status: suite.Passed, DurationMS: 12},
		{ID: "PROGRAM-004", Title: "duplicate name", Refs: []string{"Definition: Object names"}, Status: suite.Failed, DurationMS: 3,
			Messages: []string{"status 201, want 409", "second message"},
			Exchanges: []*probe.Exchange{{
				Client: "BL", Method: "POST", URL: "http://vtn/programs", RequestBody: `{"programName":"<p>"}`,
				RequestHeaders: map[string]string{"Authorization": "Bearer " + probe.Redacted},
				Status:         201, ResponseBody: `{"id":"x"}`,
			}, {Client: "BL", Method: "GET", URL: "http://vtn/gone", Error: "connection refused"}}},
		{ID: "REPORT-003", Title: "second VEN", Status: suite.Skipped, Messages: []string{"the run has no second VEN"}},
	}
	return Run{
		SchemaVersion: SchemaVersion,
		Runner:        Runner{Name: "otfabric/openadr-interop", Version: "0.0.0"},
		Specification: Specification{Name: "OpenADR", Version: "3.1.0", SHA256: "abc"},
		Target:        Target{Name: "Some VTN", Version: "1.2.3", Role: "VTN", URL: "http://vtn"},
		Deviations:    []spec.Deviation{{ID: "SPEC-310-001", Summary: "a deviation"}},
		Started:       time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC),
		DurationMS:    15,
		Summary:       Summarize(results),
		Scenarios:     results,
	}
}

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "reports")
	run := sample()
	if run.Summary != (Summary{Total: 3, Passed: 1, Failed: 1, Skipped: 1}) {
		t.Fatalf("summary = %+v", run.Summary)
	}
	if err := Write(dir, run); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back Run
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Summary != run.Summary || back.Scenarios[1].Exchanges[0].Status != 201 || back.Deviations[0].ID != "SPEC-310-001" {
		t.Errorf("results.json does not round-trip: %+v", back)
	}

	data, err = os.ReadFile(filepath.Join(dir, "junit.xml"))
	if err != nil {
		t.Fatal(err)
	}
	var ju junitSuite
	if err := xml.Unmarshal(data, &ju); err != nil {
		t.Fatalf("junit.xml is not well-formed: %v", err)
	}
	if ju.Tests != 3 || ju.Failures != 1 || ju.Skipped != 1 || len(ju.Cases) != 3 {
		t.Errorf("junit counts = %+v", ju)
	}
	failure := ju.Cases[1].Failure
	if ju.Cases[0].Failure != nil || failure == nil || failure.Message != "status 201, want 409" ||
		!strings.Contains(failure.Text, `{"programName":"<p>"}`) || !strings.Contains(failure.Text, "no response: connection refused") {
		t.Errorf("junit failure = %+v", failure)
	}
	if ju.Cases[2].Skipped == nil || ju.Cases[1].Classname != "openadr3.1.0.PROGRAM" {
		t.Errorf("junit cases = %+v", ju.Cases)
	}

	data, err = os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	md := string(data)
	for _, want := range []string{
		"# OpenADR 3.1.0 conformance: Some VTN 1.2.3",
		"1 of 3 scenarios failed. 1 passed, 1 failed, 1 skipped.",
		"not an OpenADR Alliance certification",
		`| PROGRAM-001 | creates \| a program | passed |`,
		"**SPEC-310-001**: a deviation",
		"**REPORT-003**: the run has no second VEN",
		"### PROGRAM-004 duplicate name (failed)",
		"Authorization: Bearer [redacted]",
		"=> 201",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report.md lacks %q:\n%s", want, md)
		}
	}
}
