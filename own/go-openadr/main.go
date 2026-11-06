// SPDX-License-Identifier: MIT

// Command openadr-interop is the openadr-interop-otfabric image: go-openadr
// behind the container contract (docs/CONTAINER_CONTRACT.md), as a VTN (the
// server role) and as its Business Logic and VEN clients (the client role).
//
// It is OT Fabric's own stack, not an independent implementation: results
// against it are never independent evidence for go-openadr (README.md).
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
	adapterName   = "otfabric"
	schemaVersion = "1.0"
	fixtureRoot   = "/fixtures"
	readyFile     = "/run/openadr-interop/ready"
	// The revision the VTN of go-openadr serves. Its clients speak 3.0.1
	// too, but a capability document has one list for both roles: see
	// README.md.
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
		fmt.Printf("openadr-interop %s %s (go-openadr %s)\n", adapterName, adapterVersion, upstreamVersion)
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
// only when the public API of go-openadr has it and the self-tests exercise
// it for this adapter; README.md says why each false one is false.
func capabilities() map[string]any {
	return map[string]any{
		"schemaVersion":   schemaVersion,
		"adapter":         adapterName,
		"adapterVersion":  adapterVersion,
		"protocol":        "openadr3",
		"openadrVersions": []string{openadrVersion},
		"upstream": map[string]any{
			"name":              "go-openadr",
			"version":           upstreamVersion,
			"revision":          upstreamRevision,
			"license":           "MIT",
			"language":          "Go",
			"independentEngine": false,
		},
		"roles": map[string]bool{"server": true, "client": true},
		"features": map[string]bool{
			// auth.Issuer is the VTN's token endpoint, at /auth/token.
			"serverTokenEndpoint": true,
			// ven objects carry objectType "VEN".
			"serverVenObjectType": true,
			// A token the Issuer did not issue is answered 401.
			"serverInvalidTokenIs401": true,
			// A VEN sees a targeted program or event only when its search
			// names a target it was granted.
			"serverTargetsMustBeNamed": true,
			// go-openadr publishes to a broker, and has none of its own.
			"serverMqtt": false,
			// discovery.Advertise exists; no self-test covers it.
			"serverMdns":   false,
			"mdnsDiscover": false,
			// The VTN has webhook.Sender: GET /notifiers names WEBHOOK.
			"serverNotifiersWebhook": true,
			// server.Config.ResourceGroups.
			"serverResourceGroups": true,
			// Without --token-url the clients ask GET /auth/server.
			"clientTokenDiscovery": true,
			// The program model takes report payload descriptors.
			"clientReportDescriptorsInPrograms": true,
			// The clients without Credentials send no token.
			"clientAnonymous": true,
			// ResourceGroups on both clients, writes on BL.
			"clientResourceGroups": true,
			// The MQTT subscriber hands over decoded notifications, not the
			// payload the contract asks for; no self-test covers it.
			"clientMqtt": false,
			// Subscriptions name CREATE, READ, UPDATE and DELETE.
			"clientSubscriptionOperations310": true,
			// Notifiers.MQTT is nil for a VTN without a broker.
			"clientNotifiersWithoutMqtt": true,
			// Topics.Create is empty for one program or ven.
			"clientTopicsOfOneObject": true,
		},
		"serverObjects":    []string{"programs", "events", "reports", "vens", "resources"},
		"clientOperations": map[string][]string{"bl": operationsOf("bl"), "ven": operationsOf("ven")},
	}
}
