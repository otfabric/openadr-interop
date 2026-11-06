// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	openadr "github.com/otfabric/go-openadr"
	"github.com/otfabric/go-openadr/client"
	"github.com/otfabric/go-openadr/server/storage"
)

// The kinds of object of the contract, in the order of their operations in
// the capability document.
var objectKinds = []string{"program", "event", "report", "ven", "resource", "subscription", "resource-group"}

var verbs = []string{"list", "get", "create", "update", "delete"}

// offers reports whether the library has an operation for a role. It
// follows the methods of client.BL and client.VEN: Business Logic does not
// write reports, a VEN does not write programs, events or resource groups.
func offers(role, kind, verb string) bool {
	if verb == "list" || verb == "get" {
		return true
	}
	if role == "bl" {
		return kind != "report"
	}
	return kind != "program" && kind != "event" && kind != "resource-group"
}

func operationName(verb, kind string) string {
	if verb == "list" {
		return "list-" + kind + "s"
	}
	return verb + "-" + kind
}

func operationsOf(role string) []string {
	var ops []string
	for _, kind := range objectKinds {
		for _, verb := range verbs {
			if offers(role, kind, verb) {
				ops = append(ops, operationName(verb, kind))
			}
		}
	}
	return append(ops, "get-notifiers", "get-topics")
}

// The search filters of each kind of object, besides --skip and --limit.
var listFilters = map[string][]string{
	"program":        {"targets"},
	"event":          {"targets", "program-id"},
	"report":         {"program-id", "event-id", "client-name"},
	"ven":            {"targets", "ven-name"},
	"resource":       {"targets", "ven-id", "resource-name"},
	"subscription":   {"program-id", "client-name"},
	"resource-group": {"targets", "resource-group-name"},
}

// The topic scopes of get-topics, and the object whose ID each takes.
var topicScopes = map[string]struct {
	of  string
	set func(id string) client.TopicSet
}{
	"programs":       {"", func(string) client.TopicSet { return client.AllPrograms() }},
	"program":        {"program", client.OneProgram},
	"events":         {"", func(string) client.TopicSet { return client.AllEvents() }},
	"program-events": {"program", client.EventsOfProgram},
	"reports":        {"", func(string) client.TopicSet { return client.AllReports() }},
	"subscriptions":  {"", func(string) client.TopicSet { return client.AllSubscriptions() }},
	"vens":           {"", func(string) client.TopicSet { return client.AllVENs() }},
	"ven":            {"ven", client.OneVEN},
	"resources":      {"", func(string) client.TopicSet { return client.AllResources() }},
	"ven-events":     {"ven", client.EventsForVEN},
	"ven-programs":   {"ven", client.ProgramsForVEN},
	"ven-resources":  {"ven", client.ResourcesOfVEN},
}

type clientRequest struct {
	operation, verb, kind string
	url, tokenURL         string
	timeout               time.Duration
	who                   fixtureClient
	anonymous             bool
	id                    string
	body                  []byte
	filters               map[string]string
	targets               []string
	page                  client.Page
	scope                 string
}

func parseClient(args []string) (*clientRequest, error) {
	if len(args) == 0 {
		return nil, usagef("client needs an operation")
	}
	r := &clientRequest{operation: args[0], filters: map[string]string{}}
	known := []string{"url", "as", "anonymous", "fixture", "token-url", "timeout-ms"}
	notifier := false
	switch r.operation {
	case "get-notifiers":
		notifier = true
	case "get-topics":
		notifier = true
		known = append(known, "scope", "id")
	default:
		verb, kind, ok := strings.Cut(r.operation, "-")
		if verb == "list" {
			kind, ok = strings.CutSuffix(kind, "s")
		}
		if !ok || !contains(verbs, verb) || !contains(objectKinds, kind) || operationName(verb, kind) != r.operation {
			return nil, usagef("unknown operation %q", r.operation)
		}
		r.verb, r.kind = verb, kind
		switch verb {
		case "list":
			known = append(append(known, "skip", "limit"), listFilters[kind]...)
		case "get", "delete":
			known = append(known, "id")
		case "create":
			known = append(known, "body")
		case "update":
			known = append(known, "id", "body")
		}
	}
	given, err := parseFlags(args[1:], known...)
	if err != nil {
		return nil, err
	}
	required := []string{"url"}
	if notifier {
		if r.operation == "get-topics" {
			required = append(required, "scope")
		}
	} else {
		for _, f := range []string{"id", "body"} {
			if contains(known, f) {
				required = append(required, f)
			}
		}
	}
	for _, f := range required {
		if _, ok := given[f]; !ok {
			return nil, usagef("--%s is required", f)
		}
	}
	_, as := given["as"]
	_, anonymous := given["anonymous"]
	if as == anonymous {
		return nil, usagef("one of --as and --anonymous is required")
	}
	r.url = trimSlash(given["url"])
	if !httpURL.MatchString(r.url) {
		return nil, usagef("--url: %q is not an http or https URL", given["url"])
	}
	if v, ok := given["token-url"]; ok {
		if !httpURL.MatchString(v) {
			return nil, usagef("--token-url: %q is not an http or https URL", v)
		}
		r.tokenURL = v
	}
	ms, err := number("timeout-ms", valueOr(given, "timeout-ms", "10000"), 1, 600000)
	if err != nil {
		return nil, err
	}
	r.timeout = time.Duration(ms) * time.Millisecond

	if v, ok := given["scope"]; ok {
		scope, known := topicScopes[v]
		if !known {
			return nil, usagef("--scope: %q is not a topic scope", v)
		}
		_, hasID := given["id"]
		if (scope.of == "") == hasID {
			if scope.of == "" {
				return nil, usagef("--scope %s takes no --id", v)
			}
			return nil, usagef("--scope %s takes the ID of a %s", v, scope.of)
		}
		r.scope = v
	}
	if v, ok := given["id"]; ok {
		if !openadr.ValidID(v) {
			return nil, usagef("--id: %q is not an object ID", v)
		}
		r.id = v
	}
	if v, ok := given["body"]; ok {
		var obj map[string]any
		if err := json.Unmarshal([]byte(v), &obj); err != nil || obj == nil {
			return nil, usagef("--body must be a JSON object")
		}
		r.body = []byte(v)
	}
	if r.verb == "list" {
		for _, f := range listFilters[r.kind] {
			v, ok := given[f]
			switch {
			case !ok:
			case f == "targets":
				r.targets = strings.Split(v, ",")
				if contains(r.targets, "") {
					return nil, usagef("--targets: an empty target")
				}
			case strings.HasSuffix(f, "-id") && !openadr.ValidID(v):
				return nil, usagef("--%s: %q is not an object ID", f, v)
			default:
				r.filters[f] = v
			}
		}
		_, skip := given["skip"]
		_, limit := given["limit"]
		if skip || limit {
			if r.page.Skip, err = number("skip", valueOr(given, "skip", "0"), 0, 1<<31-1); err != nil {
				return nil, err
			}
			if r.page.Limit, err = number("limit", valueOr(given, "limit", "50"), 0, 1<<31-1); err != nil {
				return nil, err
			}
		}
	}

	if anonymous {
		role := given["anonymous"]
		if role != "bl" && role != "ven" {
			return nil, usagef("--anonymous: %q is not bl or ven", role)
		}
		if r.tokenURL != "" {
			return nil, usagef("--anonymous takes no --token-url")
		}
		r.who, r.anonymous = fixtureClient{Name: "anonymous", Role: role}, true
	} else {
		f, err := loadFixture(valueOr(given, "fixture", fixtureRoot+"/baseline/fixture.json"))
		if err == nil {
			err = served(f)
		}
		if err != nil {
			return nil, usagef("--fixture: %v", err)
		}
		who, ok := f.client(given["as"])
		if !ok {
			return nil, usagef("--as: the fixture has no client named %q", given["as"])
		}
		r.who = who
	}
	if !contains(operationsOf(r.who.Role), r.operation) {
		return nil, usagef("%s is not available to a %s client of go-openadr", r.operation, r.who.Role)
	}
	return r, nil
}

// recorder sees every HTTP exchange of the library: whether a token was
// issued, the status of the operation's answer and the body of an error.
type recorder struct {
	base http.RoundTripper

	mu           sync.Mutex
	tokenIssued  bool
	apiResponses int
	status       *int
	errorBody    []byte
}

func (rec *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rec.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	path := req.URL.Path
	authPhase := strings.Contains(path, "/auth/") || req.Method == http.MethodPost && strings.Contains(path, "token")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if authPhase {
		if req.Method == http.MethodPost && resp.StatusCode/100 == 2 {
			rec.tokenIssued = true
		}
		return resp, nil
	}
	rec.apiResponses++
	status := resp.StatusCode
	rec.status = &status
	rec.errorBody = nil
	if status >= 400 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		rec.errorBody = body
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, nil
}

type resultError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type resultDocument struct {
	SchemaVersion string       `json:"schemaVersion"`
	Adapter       string       `json:"adapter"`
	Operation     string       `json:"operation"`
	Client        string       `json:"client"`
	Role          string       `json:"role"`
	OK            bool         `json:"ok"`
	Error         *resultError `json:"error"`
	Authenticated bool         `json:"authenticated"`
	Status        *int         `json:"status"`
	Response      any          `json:"response"`
	Problem       any          `json:"problem"`
	ElapsedMs     int64        `json:"elapsedMs"`
}

func runClient(args []string) int {
	r, err := parseClient(args)
	if err != nil {
		return usage(err.Error())
	}
	doc, code := execute(r)
	b, _ := json.Marshal(doc)
	fmt.Println(string(b))
	return code
}

// invalidRequest is a request body the library's model refused: nothing
// was sent.
type invalidRequest struct{ err error }

func (e invalidRequest) Error() string { return e.err.Error() }
func (e invalidRequest) Unwrap() error { return e.err }

func execute(r *clientRequest) (resultDocument, int) {
	rec := &recorder{base: http.DefaultTransport.(*http.Transport).Clone()}
	doc := resultDocument{
		SchemaVersion: schemaVersion, Adapter: adapterName, Operation: r.operation,
		Client: r.who.Name, Role: r.who.Role,
	}
	started := time.Now()
	response, err := call(r, &http.Client{Transport: rec, Timeout: r.timeout})
	doc.ElapsedMs = time.Since(started).Milliseconds()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	doc.Authenticated, doc.Status = rec.tokenIssued, rec.status
	if err == nil {
		doc.OK, doc.Response = true, response
		return doc, exitOK
	}
	code := classify(err, rec)
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	doc.Error = &resultError{Code: code, Message: message}
	if code == "http-error" && len(rec.errorBody) > 0 {
		var problem map[string]any
		if json.Unmarshal(rec.errorBody, &problem) == nil && problem != nil {
			doc.Problem = problem
		}
	}
	if code == "connect-failed" {
		return doc, exitConnect
	}
	return doc, exitFailed
}

// classify maps what the library returned to error.code of the contract.
func classify(err error, rec *recorder) string {
	var invalid invalidRequest
	if errors.As(err, &invalid) {
		return "invalid-request"
	}
	var dns *net.DNSError
	var op *net.OpError
	if errors.As(err, &dns) || errors.As(err, &op) && op.Op == "dial" {
		return "connect-failed"
	}
	var netErr net.Error
	if errors.Is(err, openadr.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var token *client.TokenError
	if errors.As(err, &token) {
		return "auth-failed"
	}
	if rec.apiResponses == 0 {
		if errors.Is(err, openadr.ErrInvalid) {
			// The library validates an object before it sends it.
			return "invalid-request"
		}
		// Before the operation's answer, only the token can have failed.
		return "auth-failed"
	}
	var answer *openadr.Error
	if errors.As(err, &answer) {
		return "http-error"
	}
	if errors.Is(err, openadr.ErrProtocol) {
		return "invalid-response"
	}
	return "failed"
}

// call performs the operation with the library's client for the role.
func call(r *clientRequest, hc *http.Client) (any, error) {
	cfg := client.Config{BaseURL: r.url, HTTPClient: hc, UserAgent: "openadr-interop-otfabric/" + adapterVersion}
	if !r.anonymous {
		cfg.Credentials = &client.Credentials{ClientID: r.who.ClientID, ClientSecret: r.who.ClientSecret, TokenURL: r.tokenURL}
	}
	ctx := context.Background()
	if r.who.Role == "bl" {
		bl, err := client.NewBL(cfg)
		if err != nil {
			return nil, err
		}
		return callBL(ctx, bl, r)
	}
	ven, err := client.NewVEN(cfg)
	if err != nil {
		return nil, err
	}
	return callVEN(ctx, ven, r)
}

// reader is what both clients have.
type reader interface {
	Programs(context.Context, client.ProgramQuery) ([]openadr.Program, error)
	Program(context.Context, string) (openadr.Program, error)
	Events(context.Context, client.EventQuery) ([]openadr.Event, error)
	Event(context.Context, string) (openadr.Event, error)
	Reports(context.Context, client.ReportQuery) ([]openadr.Report, error)
	Report(context.Context, string) (openadr.Report, error)
	VENs(context.Context, client.VENQuery) ([]openadr.VEN, error)
	VEN(context.Context, string) (openadr.VEN, error)
	CreateVEN(context.Context, openadr.VEN) (openadr.VEN, error)
	UpdateVEN(context.Context, openadr.VEN) (openadr.VEN, error)
	DeleteVEN(context.Context, string) (openadr.VEN, error)
	Resources(context.Context, client.ResourceQuery) ([]openadr.Resource, error)
	Resource(context.Context, string) (openadr.Resource, error)
	CreateResource(context.Context, openadr.Resource) (openadr.Resource, error)
	UpdateResource(context.Context, openadr.Resource) (openadr.Resource, error)
	DeleteResource(context.Context, string) (openadr.Resource, error)
	Subscriptions(context.Context, client.SubscriptionQuery) ([]openadr.Subscription, error)
	Subscription(context.Context, string) (openadr.Subscription, error)
	CreateSubscription(context.Context, openadr.Subscription) (openadr.Subscription, error)
	UpdateSubscription(context.Context, openadr.Subscription) (openadr.Subscription, error)
	DeleteSubscription(context.Context, string) (openadr.Subscription, error)
	ResourceGroups(context.Context, client.ResourceGroupQuery) ([]openadr.ResourceGroup, error)
	ResourceGroup(context.Context, string) (openadr.ResourceGroup, error)
	Notifiers(context.Context) (openadr.Notifiers, error)
	MQTTTopics(context.Context, client.TopicSet) (openadr.Topics, error)
}

func callBL(ctx context.Context, c *client.BL, r *clientRequest) (any, error) {
	switch r.kind {
	case "program":
		switch r.verb {
		case "create", "update":
			p, err := body[openadr.Program](r)
			if err != nil {
				return nil, err
			}
			if r.verb == "create" {
				return one(c.CreateProgram(ctx, p))
			}
			return one(c.UpdateProgram(ctx, p))
		case "delete":
			return one(c.DeleteProgram(ctx, r.id))
		}
	case "event":
		switch r.verb {
		case "create", "update":
			e, err := body[openadr.Event](r)
			if err != nil {
				return nil, err
			}
			if r.verb == "create" {
				return one(c.CreateEvent(ctx, e))
			}
			return one(c.UpdateEvent(ctx, e))
		case "delete":
			return one(c.DeleteEvent(ctx, r.id))
		}
	case "resource-group":
		switch r.verb {
		case "create", "update":
			g, err := body[openadr.ResourceGroup](r)
			if err != nil {
				return nil, err
			}
			if r.verb == "create" {
				return one(c.CreateResourceGroup(ctx, g))
			}
			return one(c.UpdateResourceGroup(ctx, g))
		case "delete":
			return one(c.DeleteResourceGroup(ctx, r.id))
		}
	}
	return callCommon(ctx, c, r)
}

func callVEN(ctx context.Context, c *client.VEN, r *clientRequest) (any, error) {
	if r.kind == "report" {
		switch r.verb {
		case "create", "update":
			rep, err := body[openadr.Report](r)
			if err != nil {
				return nil, err
			}
			if r.verb == "create" {
				return one(c.CreateReport(ctx, rep))
			}
			return one(c.UpdateReport(ctx, rep))
		case "delete":
			return one(c.DeleteReport(ctx, r.id))
		}
	}
	return callCommon(ctx, c, r)
}

// callCommon is what both clients offer: every read, and the writes of
// vens, resources and subscriptions.
func callCommon(ctx context.Context, c reader, r *clientRequest) (any, error) {
	switch r.operation {
	case "get-notifiers":
		n, err := c.Notifiers(ctx)
		if err != nil {
			return nil, err
		}
		return notifiersJSON(n), nil
	case "get-topics":
		t, err := c.MQTTTopics(ctx, topicScopes[r.scope].set(r.id))
		if err != nil {
			return nil, err
		}
		return topicsJSON(t), nil
	}
	page := r.page
	switch r.kind + " " + r.verb {
	case "program list":
		return many(c.Programs(ctx, client.ProgramQuery{Targets: r.targets, Page: page}))
	case "program get":
		return one(c.Program(ctx, r.id))
	case "event list":
		return many(c.Events(ctx, client.EventQuery{ProgramID: r.filters["program-id"], Targets: r.targets, Page: page}))
	case "event get":
		return one(c.Event(ctx, r.id))
	case "report list":
		return many(c.Reports(ctx, client.ReportQuery{ProgramID: r.filters["program-id"], EventID: r.filters["event-id"],
			ClientName: r.filters["client-name"], Page: page}))
	case "report get":
		return one(c.Report(ctx, r.id))
	case "ven list":
		return many(c.VENs(ctx, client.VENQuery{Name: r.filters["ven-name"], Targets: r.targets, Page: page}))
	case "ven get":
		return one(c.VEN(ctx, r.id))
	case "ven create", "ven update":
		v, err := body[openadr.VEN](r)
		if err != nil {
			return nil, err
		}
		if r.verb == "create" {
			return one(c.CreateVEN(ctx, v))
		}
		return one(c.UpdateVEN(ctx, v))
	case "ven delete":
		return one(c.DeleteVEN(ctx, r.id))
	case "resource list":
		return many(c.Resources(ctx, client.ResourceQuery{Name: r.filters["resource-name"], VENID: r.filters["ven-id"],
			Targets: r.targets, Page: page}))
	case "resource get":
		return one(c.Resource(ctx, r.id))
	case "resource create", "resource update":
		res, err := body[openadr.Resource](r)
		if err != nil {
			return nil, err
		}
		if r.verb == "create" {
			return one(c.CreateResource(ctx, res))
		}
		return one(c.UpdateResource(ctx, res))
	case "resource delete":
		return one(c.DeleteResource(ctx, r.id))
	case "subscription list":
		return subscriptions(c.Subscriptions(ctx, client.SubscriptionQuery{ProgramID: r.filters["program-id"],
			ClientName: r.filters["client-name"], Page: page}))
	case "subscription get":
		return subscription(c.Subscription(ctx, r.id))
	case "subscription create", "subscription update":
		s, err := subscriptionBody(r)
		if err != nil {
			return nil, err
		}
		if r.verb == "create" {
			return subscription(c.CreateSubscription(ctx, s))
		}
		return subscription(c.UpdateSubscription(ctx, s))
	case "subscription delete":
		return subscription(c.DeleteSubscription(ctx, r.id))
	case "resource-group list":
		return many(c.ResourceGroups(ctx, client.ResourceGroupQuery{Name: r.filters["resource-group-name"], Targets: r.targets, Page: page}))
	case "resource-group get":
		return one(c.ResourceGroup(ctx, r.id))
	}
	// Not reachable: parseClient admits only what operationsOf lists.
	return nil, fmt.Errorf("%s is not implemented", r.operation)
}

// body reads --body with the library's own codec, as the JSON form of the
// specification; an update names its object by --id.
func body[T storage.Object](r *clientRequest) (T, error) {
	v, err := storage.Decode[T](r.body)
	if err != nil {
		return v, invalidRequest{err}
	}
	switch p := any(&v).(type) {
	case *openadr.Program:
		p.ID = r.id
	case *openadr.Event:
		p.ID = r.id
	case *openadr.Report:
		p.ID = r.id
	case *openadr.VEN:
		p.ID = r.id
	case *openadr.Resource:
		p.ID = r.id
	case *openadr.ResourceGroup:
		p.ID = r.id
	}
	return v, nil
}

func subscriptionBody(r *clientRequest) (openadr.Subscription, error) {
	wrapped, _ := json.Marshal(map[string]json.RawMessage{"subscription": r.body})
	rec, err := storage.Decode[storage.SubscriptionRecord](wrapped)
	if err != nil {
		return openadr.Subscription{}, invalidRequest{err}
	}
	rec.Subscription.ID = r.id
	return rec.Subscription, nil
}

// one and many encode what the library returned in the JSON form of the
// specification, with the library's own codec.
func one[T storage.Object](v T, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	b, err := storage.Encode(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func many[T storage.Object](vs []T, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(vs))
	for _, v := range vs {
		b, err := storage.Encode(v)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func subscriptionJSON(s openadr.Subscription) (json.RawMessage, error) {
	b, err := storage.Encode(storage.SubscriptionRecord{Subscription: s})
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Subscription json.RawMessage `json:"subscription"`
	}
	if err := json.Unmarshal(b, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Subscription, nil
}

func subscription(s openadr.Subscription, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return subscriptionJSON(s)
}

func subscriptions(list []openadr.Subscription, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(list))
	for _, s := range list {
		b, err := subscriptionJSON(s)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// notifiersJSON and topicsJSON write openadr.Notifiers and openadr.Topics in
// the JSON form of OpenADR 3.1.0 (notifiersResponse, topicsResponse).
func notifiersJSON(n openadr.Notifiers) map[string]any {
	out := map[string]any{"WEBHOOK": n.Webhook}
	if n.WebSocket {
		out["WEBSOCKET"] = true
	}
	if n.MQTT != nil {
		auth := map[string]any{"method": n.MQTT.Authentication.Method}
		for k, v := range map[string]string{"username": n.MQTT.Authentication.Username, "caCert": n.MQTT.Authentication.CACert,
			"clientCert": n.MQTT.Authentication.ClientCert, "clientKey": n.MQTT.Authentication.ClientKey} {
			if v != "" {
				auth[k] = v
			}
		}
		out["MQTT"] = map[string]any{"URIS": n.MQTT.URIs, "serialization": n.MQTT.Serialization, "authentication": auth}
	}
	return out
}

func topicsJSON(t openadr.Topics) map[string]any {
	topics := map[string]string{"UPDATE": t.Update, "DELETE": t.Delete}
	if t.Create != "" {
		topics["CREATE"] = t.Create
	}
	if t.All != "" {
		topics["ALL"] = t.All
	}
	return map[string]any{"topics": topics}
}
