// Package suite runs conformance scenarios against a VTN and collects what
// happened. A scenario is ordinary Go code that talks to the VTN through
// probe clients and states its expectations through a T.
package suite

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/otfabric/openadr-interop/conformance/internal/probe"
	"github.com/otfabric/openadr-interop/conformance/spec"
)

// Status is the outcome of a scenario.
type Status string

// Outcomes of a scenario.
const (
	Passed  Status = "passed"
	Failed  Status = "failed"
	Skipped Status = "skipped"
	// Known marks a failure that is listed as a known finding about the
	// target. The runner sets it; a scenario never does.
	Known Status = "known"
)

// Scenario is one conformance test.
type Scenario struct {
	// ID is stable across runs, such as "PROGRAM-001".
	ID    string
	Title string
	// Refs name the parts of the specification the scenario checks: an
	// operationId of the OpenAPI document, or a section of the Definition.
	Refs []string
	Run  func(t *T)
}

// Env is what a scenario runs against.
type Env struct {
	Spec *spec.Doc
	// BL is authenticated as Business Logic and VEN as a VEN.
	BL, VEN *probe.Client
	// VEN2 is a second VEN, or nil if the run was not given one.
	VEN2 *probe.Client
	// Anonymous sends no token.
	Anonymous *probe.Client
	// VENClientID is the OAuth2 client ID behind VEN.
	VENClientID string
	// Nonce makes the names a run creates unique, so that a run does not
	// trip over what an earlier one left on a VTN that keeps its data.
	Nonce string
}

// Result is the outcome of one scenario.
type Result struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Refs       []string          `json:"refs"`
	Status     Status            `json:"status"`
	Messages   []string          `json:"messages,omitempty"`
	DurationMS float64           `json:"durationMs"`
	Exchanges  []*probe.Exchange `json:"exchanges"`
}

// T is a scenario's handle on the run.
type T struct {
	Env
	ctx      context.Context
	result   *Result
	cleanups []func()
}

// stop unwinds a scenario after Fatalf or Skipf.
type stop struct{}

// Context returns the context of the run.
func (t *T) Context() context.Context { return t.ctx }

// Errorf records a failure and lets the scenario continue.
func (t *T) Errorf(format string, args ...any) {
	t.result.Status = Failed
	t.result.Messages = append(t.result.Messages, fmt.Sprintf(format, args...))
}

// Fatalf records a failure and ends the scenario.
func (t *T) Fatalf(format string, args ...any) {
	t.Errorf(format, args...)
	panic(stop{})
}

// Skipf ends the scenario as skipped, unless it has already failed.
func (t *T) Skipf(format string, args ...any) {
	if t.result.Status != Failed {
		t.result.Status = Skipped
	}
	t.result.Messages = append(t.result.Messages, fmt.Sprintf(format, args...))
	panic(stop{})
}

// Cleanup registers f to run when the scenario ends, last registered first.
func (t *T) Cleanup(f func()) { t.cleanups = append(t.cleanups, f) }

// Name returns a name unique to this run, for objects whose name the VTN
// requires to be unique.
func (t *T) Name(base string) string { return base + "-" + t.Nonce }

// Call sends a request and records the exchange.
func (t *T) Call(c *probe.Client, method, path string, body any) *probe.Exchange {
	ex := c.Do(t.ctx, method, path, body)
	t.result.Exchanges = append(t.result.Exchanges, ex)
	return ex
}

// Expect requires the response to have the given status and checks its body
// against the schema the specification defines for that status of the
// operation. A different status ends the scenario; a schema fault is
// recorded and the scenario continues. It returns the decoded body.
func (t *T) Expect(ex *probe.Exchange, status int, operationID string) any {
	if ex.Error != "" {
		t.Fatalf("%s %s by %s: no response: %s", ex.Method, ex.URL, ex.Client, ex.Error)
	}
	if ex.Status != status {
		t.Fatalf("%s %s by %s: status %d, want %d; body: %s", ex.Method, ex.URL, ex.Client, ex.Status, status, clip(ex.ResponseBody))
	}
	if err := t.Spec.ValidateResponse(operationID, status, ex.Body()); err != nil {
		t.Errorf("%s %s by %s (%s, %d): %v", ex.Method, ex.URL, ex.Client, operationID, status, err)
	}
	return ex.JSON()
}

// ExpectProblem is Expect for an error response: besides the schema, the
// problem's status member, when present, has to repeat the HTTP status.
func (t *T) ExpectProblem(ex *probe.Exchange, status int, operationID string) {
	body, _ := t.Expect(ex, status, operationID).(map[string]any)
	if body == nil {
		t.Errorf("%s %s by %s: the %d response carries no problem object (Definition, Response Codes and Errors); body: %s",
			ex.Method, ex.URL, ex.Client, status, clip(ex.ResponseBody))
		return
	}
	if s, ok := body["status"].(float64); ok && int(s) != status {
		t.Errorf("%s %s by %s: problem.status is %v in a %d response", ex.Method, ex.URL, ex.Client, s, status)
	}
}

func clip(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// Object requires v to be a JSON object.
func (t *T) Object(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON object, got %T", v)
	}
	return m
}

// List requires v to be a JSON array of objects.
func (t *T) List(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		t.Fatalf("expected a JSON array, got %T", v)
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		out[i] = t.Object(item)
	}
	return out
}

// Run executes the scenarios whose ID matches filter (all of them when
// filter is nil), one after the other.
func Run(ctx context.Context, env Env, scenarios []Scenario, filter *regexp.Regexp) []Result {
	var results []Result
	for _, sc := range scenarios {
		if filter != nil && !filter.MatchString(sc.ID) {
			continue
		}
		results = append(results, runOne(ctx, env, sc))
	}
	return results
}

func runOne(ctx context.Context, env Env, sc Scenario) (result Result) {
	result = Result{ID: sc.ID, Title: sc.Title, Refs: sc.Refs, Status: Passed, Exchanges: []*probe.Exchange{}}
	t := &T{Env: env, ctx: ctx, result: &result}
	start := time.Now()
	defer func() { result.DurationMS = float64(time.Since(start).Microseconds()) / 1000 }()
	// Cleanups run even when the scenario stops early, and a failure in one
	// must not stop the others.
	defer func() {
		for i := len(t.cleanups) - 1; i >= 0; i-- {
			func() {
				defer func() { _ = recover() }()
				t.cleanups[i]()
			}()
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(stop); !ok {
				result.Status = Failed
				result.Messages = append(result.Messages, fmt.Sprintf("the scenario panicked: %v", r))
			}
		}
	}()
	sc.Run(t)
	return result
}

// Delete removes an object when the scenario ends, without judging the
// outcome: the scenario may have deleted it already.
func (t *T) Delete(c *probe.Client, path string) {
	t.Cleanup(func() {
		ex := c.Do(t.ctx, http.MethodDelete, path, nil)
		ex.Client += " (cleanup)"
		t.result.Exchanges = append(t.result.Exchanges, ex)
	})
}
