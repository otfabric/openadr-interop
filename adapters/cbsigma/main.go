// SPDX-License-Identifier: MIT

// Command openadr-interop is the openadr-interop-cbsigma image: cB-Sigma
// openadr (OpenADR 3, Go) behind the container contract
// (docs/CONTAINER_CONTRACT.md), as a VTN (the server role, package
// oadr3/vtn) and as Business Logic and VEN clients (the client role, the
// library's oadr3.APIClient).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	adapterName   = "cbsigma"
	schemaVersion = "1.0"
	fixtureRoot   = "/fixtures"
	readyFile     = "/run/openadr-interop/ready"
	// The revision the adapter serves and speaks. The VTN of cB-Sigma
	// openadr serves 3.0 too (Config.Version); the adapter does not offer
	// it yet.
	openadrVersion = "3.1.0"
)

// Set with -ldflags by the Dockerfile.
var (
	adapterVersion   = "dev"
	upstreamVersion  = "dev"
	upstreamRevision = "dev"
)

const (
	exitOK             = 0
	exitFailed         = 1
	exitUsage          = 2
	exitConnect        = 3
	exitInvalidFixture = 4
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return usage("a command is required")
	}
	command, rest := args[0], args[1:]
	switch command {
	case "server":
		return runServer(rest)
	case "client":
		return runClient(rest)
	case "print-capabilities":
		if len(rest) > 0 {
			return usage("print-capabilities takes no arguments")
		}
		return printJSON(capabilities())
	case "print-fixture":
		return printFixture(rest)
	case "version":
		if len(rest) > 0 {
			return usage("version takes no arguments")
		}
		fmt.Printf("openadr-interop %s %s (cB-Sigma openadr %s)\n", adapterName, adapterVersion, upstreamVersion)
		return exitOK
	}
	return usage(fmt.Sprintf("unknown command %q", command))
}

const usageText = `usage:
  openadr-interop server [--fixture PATH] [--bind-address ADDR] [--port N] [--public-url URL] [--ready-file PATH]
  openadr-interop client <operation> --url URL (--as CLIENT | --anonymous ROLE) [--fixture PATH]
                         [--token-url URL] [--timeout-ms N] [operation flags]
  openadr-interop print-capabilities
  openadr-interop print-fixture [NAME]
  openadr-interop version`

// usageError is exit 2: nothing was started and nothing was sent.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

func usage(message string) int {
	fmt.Fprintf(os.Stderr, "openadr-interop: %s\n%s\n", message, usageText)
	return exitUsage
}

func printJSON(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, "openadr-interop:", err)
		return exitFailed
	}
	fmt.Println(string(b))
	return exitOK
}

var fixtureName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func printFixture(args []string) int {
	name := "baseline"
	switch {
	case len(args) == 1:
		name = args[0]
	case len(args) > 1:
		return usage("print-fixture takes at most one name")
	}
	if !fixtureName.MatchString(name) {
		fmt.Fprintf(os.Stderr, "openadr-interop: no fixture named %q\n", name)
		return exitFailed
	}
	b, err := os.ReadFile(fixtureRoot + "/" + name + "/fixture.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "openadr-interop: no fixture named %q\n", name)
		return exitFailed
	}
	_, _ = os.Stdout.Write(b)
	return exitOK
}

// parseFlags reads "--name value" pairs. An unknown flag, a flag without a
// value and a flag given twice are usage errors.
func parseFlags(args []string, known ...string) (map[string]string, error) {
	given := map[string]string{}
	for i := 0; i < len(args); i += 2 {
		name, ok := strings.CutPrefix(args[i], "--")
		if !ok || !contains(known, name) {
			return nil, usagef("unknown flag or argument %q", args[i])
		}
		if i+1 >= len(args) {
			return nil, usagef("%s needs a value", args[i])
		}
		if _, dup := given[name]; dup {
			return nil, usagef("%s is given more than once", args[i])
		}
		given[name] = args[i+1]
	}
	return given, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// number parses a flag value within low..high.
func number(flag, text string, low, high int) (int, error) {
	n, err := strconv.Atoi(text)
	if err != nil || strings.HasPrefix(text, "+") || n < low || n > high {
		return 0, usagef("--%s: %q is not a number within %d..%d", flag, text, low, high)
	}
	return n, nil
}

var httpURL = regexp.MustCompile(`^https?://[^/\s]+(/\S*)?$`)

func isUsage(err error) bool {
	var u usageError
	return errors.As(err, &u)
}

// capabilities is the document of docs/CAPABILITIES.md. A feature is true
// only when the library has it and the self-tests exercise it for this
// adapter; docs/CAPABILITIES.md says why each false one is false.
func capabilities() map[string]any {
	return map[string]any{
		"schemaVersion":   schemaVersion,
		"adapter":         adapterName,
		"adapterVersion":  adapterVersion,
		"protocol":        "openadr3",
		"openadrVersions": []string{openadrVersion},
		"upstream": map[string]any{
			"name":              "cB-Sigma openadr",
			"version":           upstreamVersion,
			"revision":          upstreamRevision,
			"license":           "Apache-2.0",
			"language":          "Go",
			"independentEngine": true,
		},
		"roles": map[string]bool{"server": true, "client": true},
		"features": map[string]bool{
			// The VTN issues tokens at /auth/token (Config.Clients).
			"serverTokenEndpoint": true,
			// ven objects carry objectType "VEN".
			"serverVenObjectType": true,
			// A token the VTN did not issue is answered 401.
			"serverInvalidTokenIs401": true,
			// A VEN sees a targeted program or event only when its search
			// names a target it was granted.
			"serverTargetsMustBeNamed": true,
			// For a VEN, the targets a request names unlock the targeted
			// objects it was granted and narrow nothing: a search that names
			// targets returns the objects without targets too, and a
			// targeted object is read by ID only with its target in the
			// query (contract 1.5).
			"serverTargetsUnlock": true,
			// The MQTT notifier publishes through a broker client the
			// application supplies (vtn.Publisher); the image has no broker.
			"serverMqtt": false,
			// oadr3/mdns announces and browses; the adapter does not wire it.
			"serverMdns":   false,
			"mdnsDiscover": false,
			// GET /notifiers names WEBHOOK.
			"serverNotifiersWebhook": true,
			// The VTN has no /resource_groups (an extension to 3.1.0).
			"serverResourceGroups": false,
			// Without --token-url the client asks GET /auth/server.
			"clientTokenDiscovery": true,
			// The client keeps objects as JSON maps: any payload descriptor.
			"clientReportDescriptorsInPrograms": true,
			// Without credentials the client sends no token.
			"clientAnonymous": true,
			// The library has no resource groups.
			"clientResourceGroups": false,
			// The library has no MQTT client.
			"clientMqtt": false,
			// listen-webhook (contract 1.5): the subscription is written with
			// the library's client; the receiver is the adapter's, as the
			// library has none.
			"clientWebhook": true,
			// Subscriptions name CREATE, READ, UPDATE and DELETE.
			"clientSubscriptionOperations310": true,
			// The client reads GET /notifiers as a JSON object.
			"clientNotifiersWithoutMqtt": true,
			// The client reads the topics of one program or ven.
			"clientTopicsOfOneObject": true,
		},
		"serverObjects":    []string{"programs", "events", "reports", "vens", "resources"},
		"clientOperations": map[string][]string{"bl": operationsOf("bl"), "ven": operationsOf("ven")},
	}
}
