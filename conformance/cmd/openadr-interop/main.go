// Openadr-interop runs the OpenADR 3.1.0 conformance scenarios against a
// VTN and writes the results as JSON, JUnit XML and Markdown.
//
//	OPENADR_INTEROP_BL_SECRET=... OPENADR_INTEROP_VEN_SECRET=... \
//	openadr-interop -url https://vtn.example/openadr3 \
//	    -bl-client-id utility -ven-client-id site-42 \
//	    -target-name "Some VTN" -target-version 1.2.3
//
// Client secrets are read from the environment so that they stay out of
// process listings and shell history. The exit status is 0 when no scenario
// failed, 1 when one did, and 2 when the run could not start.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/otfabric/openadr-interop/conformance/internal/probe"
	"github.com/otfabric/openadr-interop/conformance/internal/report"
	"github.com/otfabric/openadr-interop/conformance/internal/suite"
	"github.com/otfabric/openadr-interop/conformance/scenarios"
	"github.com/otfabric/openadr-interop/conformance/spec"
)

// version of the test suite; scripts/check-versions.sh keeps it in step with
// interop_version of versions.yaml.
const version = "0.4.0"

type options struct {
	url, tokenURL             string
	targetName, targetVersion string
	blID, venID, ven2ID       string
	out, run                  string
	known, knownVersion       string
	timeout                   time.Duration
	list                      bool
}

func main() {
	var o options
	flag.StringVar(&o.url, "url", "", "URL of the VTN's API, the one /programs is appended to (required)")
	flag.StringVar(&o.tokenURL, "token-url", "", "OAuth2 token endpoint; discovered with GET /auth/server when empty")
	flag.StringVar(&o.targetName, "target-name", "unnamed VTN", "name of the implementation under test, for the reports")
	flag.StringVar(&o.targetVersion, "target-version", "unknown", "version of the implementation under test, for the reports")
	flag.StringVar(&o.blID, "bl-client-id", "", "client ID with the Business Logic scopes; its secret is $OPENADR_INTEROP_BL_SECRET (required)")
	flag.StringVar(&o.venID, "ven-client-id", "", "client ID with the VEN scopes; its secret is $OPENADR_INTEROP_VEN_SECRET (required)")
	flag.StringVar(&o.ven2ID, "ven2-client-id", "", "a second VEN client ID; its secret is $OPENADR_INTEROP_VEN2_SECRET (optional)")
	flag.StringVar(&o.out, "out", "reports", "directory to write results.json, junit.xml and report.md to")
	flag.StringVar(&o.run, "run", "", "run only the scenarios whose ID matches this regular expression")
	flag.StringVar(&o.known, "known", "", "file of scenarios known to fail against the target: tab-separated version, scenario ID, text of the failure, description")
	flag.StringVar(&o.knownVersion, "known-version", "", "the target version whose entries of -known apply (default: -target-version)")
	flag.DurationVar(&o.timeout, "timeout", 2*time.Minute, "time limit for the whole run")
	flag.BoolVar(&o.list, "list", false, "list the scenarios and exit")
	flag.Parse()

	if o.list {
		for _, sc := range scenarios.All() {
			fmt.Printf("%-14s %s\n", sc.ID, sc.Title)
		}
		return
	}
	failed, err := run(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "openadr-interop:", err)
		os.Exit(2)
	}
	if failed {
		os.Exit(1)
	}
}

// knownFinding is a failure that is expected of one version of the target.
type knownFinding struct {
	// match has to occur in the messages of the failed scenario.
	match   string
	summary string
}

// loadKnown reads the scenarios known to fail against one version of the
// target, by scenario ID.
func loadKnown(path, version string) (map[string]knownFinding, error) {
	known := map[string]knownFinding{}
	if path == "" {
		return known, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("-known: %w", err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || fields[2] == "" {
			return nil, fmt.Errorf("-known: %s line %d: want version, scenario ID, text of the failure and description, separated by tabs", path, i+1)
		}
		if fields[0] == version {
			known[fields[1]] = knownFinding{match: fields[2], summary: fields[3]}
		}
	}
	return known, nil
}

func run(o options) (failed bool, err error) {
	if o.url == "" || o.blID == "" || o.venID == "" {
		return false, errors.New("-url, -bl-client-id and -ven-client-id are required")
	}
	base, err := url.Parse(o.url)
	if err != nil || !base.IsAbs() {
		return false, fmt.Errorf("-url %q is not an absolute URL", o.url)
	}
	baseURL := strings.TrimSuffix(o.url, "/")
	var filter *regexp.Regexp
	if o.run != "" {
		if filter, err = regexp.Compile(o.run); err != nil {
			return false, fmt.Errorf("-run: %w", err)
		}
	}
	doc, err := spec.Load("3.1.0")
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	// Redirects are reported, not followed: none is part of the API.
	hc := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	started := time.Now()
	var setup []*probe.Exchange

	anonymous := &probe.Client{Name: "anonymous", BaseURL: baseURL, HTTP: hc}
	tokenURL := o.tokenURL
	if tokenURL == "" {
		ex := anonymous.Do(ctx, http.MethodGet, "/auth/server", nil)
		setup = append(setup, ex)
		var info struct {
			TokenURL string `json:"tokenURL"`
		}
		if ex.Error != "" || ex.Status != http.StatusOK || json.Unmarshal(ex.Body(), &info) != nil || info.TokenURL == "" {
			return false, fmt.Errorf("could not discover the token endpoint (%s); pass -token-url", ex)
		}
		ref, err := url.Parse(info.TokenURL)
		if err != nil {
			return false, fmt.Errorf("GET /auth/server named the token endpoint %q: %w", info.TokenURL, err)
		}
		// The VTN may name its own endpoint relative to the API.
		dir := *base
		dir.Path = strings.TrimSuffix(dir.Path, "/") + "/"
		tokenURL = dir.ResolveReference(ref).String()
	}

	login := func(name, id, secretEnv string) (*probe.Client, error) {
		secret := os.Getenv(secretEnv)
		if secret == "" {
			return nil, fmt.Errorf("$%s, the secret of client %q, is not set", secretEnv, id)
		}
		token, ex := probe.FetchToken(ctx, hc, name, tokenURL, id, secret)
		setup = append(setup, ex)
		if token == "" {
			return nil, fmt.Errorf("no token for client %q: %s", id, ex)
		}
		return anonymous.As(name, token), nil
	}
	env := suite.Env{Spec: doc, Anonymous: anonymous, VENClientID: o.venID}
	if env.BL, err = login("BL", o.blID, "OPENADR_INTEROP_BL_SECRET"); err != nil {
		return false, err
	}
	if env.VEN, err = login("VEN", o.venID, "OPENADR_INTEROP_VEN_SECRET"); err != nil {
		return false, err
	}
	if o.ven2ID != "" {
		if env.VEN2, err = login("VEN2", o.ven2ID, "OPENADR_INTEROP_VEN2_SECRET"); err != nil {
			return false, err
		}
	}
	nonce := make([]byte, 4)
	if _, err := rand.Read(nonce); err != nil {
		return false, err
	}
	env.Nonce = hex.EncodeToString(nonce)

	results := suite.Run(ctx, env, scenarios.All(), filter)
	version := o.knownVersion
	if version == "" {
		version = o.targetVersion
	}
	known, err := loadKnown(o.known, version)
	if err != nil {
		return false, err
	}
	// A known finding that shows is reported as such; one that does not is
	// a failure, so that the list cannot outlive what it describes.
	var findings []report.KnownFinding
	var stale []string
	for i := range results {
		finding, listed := known[results[i].ID]
		if !listed {
			continue
		}
		switch results[i].Status {
		case suite.Failed:
			// Only the failure that was recorded: a scenario that fails for
			// another reason is a new failure.
			if strings.Contains(strings.Join(results[i].Messages, "\n"), finding.match) {
				results[i].Status = suite.Known
				findings = append(findings, report.KnownFinding{ID: results[i].ID, Summary: finding.summary})
			}
		case suite.Passed:
			stale = append(stale, results[i].ID)
		case suite.Skipped, suite.Known:
		}
	}
	out := report.Run{
		SchemaVersion: report.SchemaVersion,
		Runner:        report.Runner{Name: "otfabric/openadr-interop", Version: version},
		Specification: report.Specification{Name: "OpenADR", Version: doc.Version, SHA256: doc.SHA256},
		Target:        report.Target{Name: o.targetName, Version: o.targetVersion, Role: "VTN", URL: baseURL},
		Deviations:    doc.Deviations(),
		KnownFindings: findings,
		Started:       started.UTC(),
		DurationMS:    float64(time.Since(started).Microseconds()) / 1000,
		Summary:       report.Summarize(results),
		Setup:         setup,
		Scenarios:     results,
	}
	if err := report.Write(o.out, out); err != nil {
		return false, err
	}

	for _, r := range results {
		fmt.Printf("%-8s %-14s %s\n", r.Status, r.ID, r.Title)
		if r.Status != suite.Passed {
			for _, m := range r.Messages {
				fmt.Printf("         %s\n", m)
			}
		}
	}
	s := out.Summary
	fmt.Printf("\n%d scenarios: %d passed, %d failed, %d known findings, %d skipped. Reports in %s/\n",
		s.Total, s.Passed, s.Failed, s.Known, s.Skipped, o.out)
	if s.Total == 0 {
		return false, errors.New("no scenario matched -run")
	}
	for _, id := range stale {
		fmt.Printf("%s is listed in %s but passes: remove the entry\n", id, o.known)
	}
	return s.Failed > 0 || len(stale) > 0, nil
}
