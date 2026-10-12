// Package report writes the results of a run as JSON, JUnit XML and
// Markdown.
package report

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/otfabric/openadr-interop/conformance/internal/probe"
	"github.com/otfabric/openadr-interop/conformance/internal/suite"
	"github.com/otfabric/openadr-interop/conformance/spec"
)

// SchemaVersion is the version of the JSON document Run marshals to.
const SchemaVersion = "1.0"

// Run is everything a report says about one execution of the suite.
type Run struct {
	SchemaVersion string        `json:"schemaVersion"`
	Runner        Runner        `json:"runner"`
	Specification Specification `json:"specification"`
	Target        Target        `json:"target"`
	// Deviations are the places where the assertions knowingly depart from
	// the letter of the specification.
	Deviations []spec.Deviation `json:"deviations"`
	// KnownFindings are the failed scenarios that were listed as known for
	// this version of the target.
	KnownFindings []KnownFinding `json:"knownFindings"`
	Started       time.Time      `json:"started"`
	DurationMS    float64        `json:"durationMs"`
	Summary       Summary        `json:"summary"`
	// Setup holds the exchanges made before the scenarios: token endpoint
	// discovery and the token requests.
	Setup     []*probe.Exchange `json:"setup"`
	Scenarios []suite.Result    `json:"scenarios"`
}

// KnownFinding is a scenario that fails against the target as expected.
type KnownFinding struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// Runner identifies this test suite.
type Runner struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Specification identifies the document the assertions were made against.
type Specification struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// Target is the implementation under test.
type Target struct {
	// Name and Version are as given by whoever started the run; the suite
	// cannot verify them.
	Name    string `json:"name"`
	Version string `json:"version"`
	Role    string `json:"role"`
	URL     string `json:"url"`
}

// Summary counts scenarios by outcome.
type Summary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	// Known counts failures listed as known findings; they are not in Failed.
	Known int `json:"known"`
}

// Summarize counts the outcomes of results.
func Summarize(results []suite.Result) Summary {
	s := Summary{Total: len(results)}
	for _, r := range results {
		switch r.Status {
		case suite.Passed:
			s.Passed++
		case suite.Failed:
			s.Failed++
		case suite.Skipped:
			s.Skipped++
		case suite.Known:
			s.Known++
		}
	}
	return s
}

// Write writes results.json, junit.xml and report.md into dir.
func Write(dir string, run Run) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	junit, err := JUnit(run)
	if err != nil {
		return err
	}
	for name, content := range map[string][]byte{
		"results.json": append(data, '\n'),
		"junit.xml":    junit,
		"report.md":    []byte(Markdown(run)),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// --- JUnit

type junitSuite struct {
	XMLName   xml.Name    `xml:"testsuite"`
	Name      string      `xml:"name,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Skipped   int         `xml:"skipped,attr"`
	Time      string      `xml:"time,attr"`
	Timestamp string      `xml:"timestamp,attr"`
	Props     []junitProp `xml:"properties>property"`
	Cases     []junitCase `xml:"testcase"`
}

type junitProp struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitMessage `xml:"failure,omitempty"`
	Skipped   *junitMessage `xml:"skipped,omitempty"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func seconds(ms float64) string { return fmt.Sprintf("%.3f", ms/1000) }

// JUnit renders the run as a JUnit XML test suite.
func JUnit(run Run) ([]byte, error) {
	s := junitSuite{
		Name:      fmt.Sprintf("OpenADR %s conformance: %s %s", run.Specification.Version, run.Target.Name, run.Target.Version),
		Tests:     run.Summary.Total,
		Failures:  run.Summary.Failed,
		Skipped:   run.Summary.Skipped + run.Summary.Known,
		Time:      seconds(run.DurationMS),
		Timestamp: run.Started.UTC().Format("2006-01-02T15:04:05"),
		Props: []junitProp{
			{"runner", run.Runner.Name + " " + run.Runner.Version},
			{"specification", run.Specification.Name + " " + run.Specification.Version},
			{"specification.sha256", run.Specification.SHA256},
			{"target", run.Target.Name + " " + run.Target.Version},
			{"target.role", run.Target.Role},
			{"target.url", run.Target.URL},
		},
	}
	for _, r := range run.Scenarios {
		group, _, _ := strings.Cut(r.ID, "-")
		c := junitCase{Name: r.ID + " " + r.Title, Classname: "openadr" + run.Specification.Version + "." + group, Time: seconds(r.DurationMS)}
		switch r.Status {
		case suite.Failed:
			c.Failure = &junitMessage{Message: r.Messages[0], Text: details(r)}
		case suite.Skipped:
			c.Skipped = &junitMessage{Message: strings.Join(r.Messages, "; ")}
		case suite.Known:
			// JUnit has no such outcome: not a failure of this run, not a pass.
			c.Skipped = &junitMessage{Message: "known finding: " + r.Messages[0], Text: details(r)}
		case suite.Passed:
		}
		s.Cases = append(s.Cases, c)
	}
	out, err := xml.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(out, '\n')...), nil
}

// details lists the failures of a scenario and the exchanges that led to them.
func details(r suite.Result) string {
	var b strings.Builder
	for _, m := range r.Messages {
		b.WriteString("- " + m + "\n")
	}
	b.WriteString("\nSpecification: " + strings.Join(r.Refs, ", ") + "\n\nExchanges:\n")
	for _, ex := range r.Exchanges {
		b.WriteString(exchange(ex))
	}
	return b.String()
}

func exchange(ex *probe.Exchange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n[%s] %s %s\n", ex.Client, ex.Method, ex.URL)
	if auth := ex.RequestHeaders["Authorization"]; auth != "" {
		fmt.Fprintf(&b, "Authorization: %s\n", auth)
	}
	if ex.RequestBody != "" {
		b.WriteString(ex.RequestBody + "\n")
	}
	if ex.Error != "" {
		fmt.Fprintf(&b, "=> no response: %s\n", ex.Error)
		return b.String()
	}
	fmt.Fprintf(&b, "=> %d\n", ex.Status)
	for _, h := range []string{"WWW-Authenticate", "Allow"} {
		if v := ex.ResponseHeaders[h]; v != "" {
			fmt.Fprintf(&b, "%s: %s\n", h, v)
		}
	}
	if ex.ResponseBody != "" {
		b.WriteString(ex.ResponseBody + "\n")
	}
	return b.String()
}

// --- Markdown

// cell makes text safe for a Markdown table cell.
func cell(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
}

// Markdown renders the run as a report for people.
func Markdown(run Run) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# OpenADR %s conformance: %s %s\n\n", run.Specification.Version, run.Target.Name, run.Target.Version)
	verdict := "All scenarios that ran passed."
	if run.Summary.Known > 0 {
		verdict = "No scenario failed unexpectedly."
	}
	if run.Summary.Failed > 0 {
		verdict = fmt.Sprintf("%d of %d scenarios failed.", run.Summary.Failed, run.Summary.Total)
	}
	fmt.Fprintf(&b, "%s %d passed, %d failed, %d skipped", verdict, run.Summary.Passed, run.Summary.Failed, run.Summary.Skipped)
	if run.Summary.Known > 0 {
		fmt.Fprintf(&b, ", %d known findings", run.Summary.Known)
	}
	b.WriteString(".\n\n")
	b.WriteString("This is the result of an independent test suite. It is not an OpenADR Alliance certification.\n\n")
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Implementation under test | %s %s (%s) |\n", cell(run.Target.Name), cell(run.Target.Version), run.Target.Role)
	fmt.Fprintf(&b, "| URL | `%s` |\n", run.Target.URL)
	fmt.Fprintf(&b, "| Specification | %s %s, OpenAPI document SHA-256 `%s` |\n", run.Specification.Name, run.Specification.Version, run.Specification.SHA256)
	fmt.Fprintf(&b, "| Test suite | %s %s |\n", run.Runner.Name, run.Runner.Version)
	fmt.Fprintf(&b, "| Started | %s, took %.2f s |\n\n", run.Started.UTC().Format(time.RFC3339), run.DurationMS/1000)

	b.WriteString("## Scenarios\n\n| ID | Scenario | Result | Specification |\n|---|---|---|---|\n")
	for _, r := range run.Scenarios {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r.ID, cell(r.Title), r.Status, cell(strings.Join(r.Refs, ", ")))
	}

	if len(run.Deviations) > 0 {
		b.WriteString("\n## Deviations from the specification\n\nWhere the specification cannot be met as written, the suite asserts the following instead.\n\n")
		for _, d := range run.Deviations {
			fmt.Fprintf(&b, "- **%s**: %s\n", d.ID, d.Summary)
		}
	}

	var skipped, failed []suite.Result
	for _, r := range run.Scenarios {
		switch r.Status {
		case suite.Skipped:
			skipped = append(skipped, r)
		case suite.Failed, suite.Known:
			failed = append(failed, r)
		case suite.Passed:
		}
	}
	if len(run.KnownFindings) > 0 {
		b.WriteString("\n## Known findings\n\nThese scenarios fail against this version of the implementation, as recorded before the run.\n\n")
		for _, k := range run.KnownFindings {
			fmt.Fprintf(&b, "- **%s**: %s\n", k.ID, k.Summary)
		}
	}
	if len(skipped) > 0 {
		b.WriteString("\n## Skipped\n\n")
		for _, r := range skipped {
			fmt.Fprintf(&b, "- **%s**: %s\n", r.ID, strings.Join(r.Messages, "; "))
		}
	}
	if len(failed) > 0 {
		b.WriteString("\n## Failures and known findings in detail\n")
		for _, r := range failed {
			fmt.Fprintf(&b, "\n### %s %s (%s)\n\n", r.ID, r.Title, r.Status)
			for _, m := range r.Messages {
				fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(m, "\n", " "))
			}
			b.WriteString("\nHTTP exchanges, credentials redacted:\n\n````text")
			for _, ex := range r.Exchanges {
				b.WriteString(exchange(ex))
			}
			b.WriteString("````\n")
		}
	}
	return b.String()
}
