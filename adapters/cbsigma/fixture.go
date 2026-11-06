// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
)

// The rules of scripts/validate-fixtures.py and docs/FIXTURES.md, as the
// Python adapters implement them in openadr_interop/fixture.py.

type fixtureError struct{ msg string }

func (e fixtureError) Error() string { return e.msg }

func fixturef(format string, a ...any) error { return fixtureError{fmt.Sprintf(format, a...)} }

var (
	labelPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	clientIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	secretPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)
	fixtureVersions = []string{"3.1.0", "3.0.1"}
	topLevel        = []string{"schemaVersion", "name", "description", "openadrVersion", "clients", "vens", "resources", "programs", "events"}
)

type fixtureClient struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// fixture is a validated fixture. Programs and events are kept as the
// objects of the file, which are requests in the JSON form of the
// specification.
type fixture struct {
	Name      string
	Version   string
	Clients   []fixtureClient
	VENs      []map[string]any
	Resources []map[string]any
	Programs  []map[string]any
	Events    []map[string]any
}

func (f *fixture) client(name string) (fixtureClient, bool) {
	for _, c := range f.Clients {
		if c.Name == name {
			return c, true
		}
	}
	return fixtureClient{}, false
}

// loadFixture reads and validates the fixture at path, which is
// <dir>/<name>/fixture.json.
func loadFixture(path string) (*fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fixturef("cannot read %s: %v", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fixturef("%s: %v", path, err)
	}
	return parseFixture(data, filepath.Base(filepath.Dir(abs)))
}

func parseFixture(data []byte, directory string) (*fixture, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fixturef("not JSON: %v", err)
	}
	if dec.More() {
		return nil, fixturef("not JSON: more than one value")
	}
	top, ok := doc.(map[string]any)
	if !ok {
		return nil, fixturef("the fixture must be a JSON object")
	}
	for k := range top {
		if !slices.Contains(topLevel, k) {
			return nil, fixturef("unknown property %q", k)
		}
	}
	if top["schemaVersion"] != schemaVersion {
		return nil, fixturef("schemaVersion must be %q", schemaVersion)
	}
	name, _ := top["name"].(string)
	if !labelPattern.MatchString(name) {
		return nil, fixturef("name must be lower-case letters, digits and hyphens")
	}
	if directory != "" && name != directory {
		return nil, fixturef("name is %q but the fixture is in the directory %q", name, directory)
	}
	if d, has := top["description"]; has {
		if _, ok := d.(string); !ok {
			return nil, fixturef("description must be a string")
		}
	}
	version, _ := top["openadrVersion"].(string)
	if !slices.Contains(fixtureVersions, version) {
		return nil, fixturef("openadrVersion must be one of %v", fixtureVersions)
	}
	f := &fixture{Name: name, Version: version}

	clients, err := objects(top, "clients", version, []string{"name", "role", "clientId", "clientSecret"}, []string{"name", "role", "clientId", "clientSecret"})
	if err != nil {
		return nil, err
	}
	roles := map[string]string{}
	ids := map[string]bool{}
	for i, c := range clients {
		var fc fixtureClient
		fc.Name, _ = c["name"].(string)
		fc.Role, _ = c["role"].(string)
		fc.ClientID, _ = c["clientId"].(string)
		fc.ClientSecret, _ = c["clientSecret"].(string)
		switch {
		case !labelPattern.MatchString(fc.Name):
			return nil, fixturef("clients[%d].name must be lower-case letters, digits and hyphens", i)
		case fc.Role != "bl" && fc.Role != "ven":
			return nil, fixturef("clients[%d].role must be bl or ven", i)
		case !clientIDPattern.MatchString(fc.ClientID):
			return nil, fixturef("clients[%d].clientId must be 1 to 128 characters of [a-zA-Z0-9_-]", i)
		case !secretPattern.MatchString(fc.ClientSecret):
			return nil, fixturef("clients[%d].clientSecret must be 8 to 128 characters of [a-zA-Z0-9_-]", i)
		}
		if _, dup := roles[fc.Name]; dup {
			return nil, fixturef("client name %q is used more than once", fc.Name)
		}
		if ids[fc.ClientID] {
			return nil, fixturef("clientId %q is used more than once", fc.ClientID)
		}
		roles[fc.Name], ids[fc.ClientID] = fc.Role, true
		f.Clients = append(f.Clients, fc)
	}
	hasBL := false
	for _, r := range roles {
		hasBL = hasBL || r == "bl"
	}
	if !hasBL {
		return nil, fixturef("there must be a client with the role bl: it creates the other objects")
	}

	if f.VENs, err = objects(top, "vens", version, []string{"venName", "client"}, []string{"venName", "client", "targets"}); err != nil {
		return nil, err
	}
	venNames := map[string]bool{}
	for i, v := range f.VENs {
		n, err := label(fmt.Sprintf("vens[%d].venName", i), v["venName"])
		if err != nil {
			return nil, err
		}
		client, _ := v["client"].(string)
		if roles[client] != "ven" {
			return nil, fixturef("vens[%d].client %q is not a client with the role ven", i, client)
		}
		if venNames[n] {
			return nil, fixturef("venName %q is used more than once", n)
		}
		venNames[n] = true
	}

	if f.Resources, err = objects(top, "resources", version, []string{"resourceName", "ven"}, []string{"resourceName", "ven", "targets"}); err != nil {
		return nil, err
	}
	resources := map[[2]string]bool{}
	for i, r := range f.Resources {
		n, err := label(fmt.Sprintf("resources[%d].resourceName", i), r["resourceName"])
		if err != nil {
			return nil, err
		}
		ven, _ := r["ven"].(string)
		if !venNames[ven] {
			return nil, fixturef("resources[%d].ven %q is not a venName of the fixture", i, ven)
		}
		if resources[[2]string{ven, n}] {
			return nil, fixturef("resource %q of the ven %q is used more than once", n, ven)
		}
		resources[[2]string{ven, n}] = true
	}

	if f.Programs, err = objects(top, "programs", version, []string{"programName"}, nil); err != nil {
		return nil, err
	}
	programs := map[string]bool{}
	for i, p := range f.Programs {
		n, err := label(fmt.Sprintf("programs[%d].programName", i), p["programName"])
		if err != nil {
			return nil, err
		}
		if programs[n] {
			return nil, fixturef("programName %q is used more than once", n)
		}
		programs[n] = true
	}

	if f.Events, err = objects(top, "events", version, []string{"eventName", "program"}, nil); err != nil {
		return nil, err
	}
	events := map[string]bool{}
	for i, e := range f.Events {
		n, err := label(fmt.Sprintf("events[%d].eventName", i), e["eventName"])
		if err != nil {
			return nil, err
		}
		if _, has := e["programID"]; has {
			return nil, fixturef("events[%d] names its program with program, not programID", i)
		}
		program, _ := e["program"].(string)
		if !programs[program] {
			return nil, fixturef("events[%d].program %q is not a programName of the fixture", i, program)
		}
		if events[n] {
			return nil, fixturef("eventName %q is used more than once", n)
		}
		events[n] = true
	}
	return f, nil
}

func label(where string, v any) (string, error) {
	s, ok := v.(string)
	if !ok || len([]rune(s)) < 1 || len([]rune(s)) > 128 {
		return "", fixturef("%s must be a string of 1 to 128 characters", where)
	}
	return s, nil
}

func objects(top map[string]any, key, version string, required, allowed []string) ([]map[string]any, error) {
	list, ok := top[key].([]any)
	if !ok {
		return nil, fixturef("%s must be a list", key)
	}
	out := make([]map[string]any, 0, len(list))
	for i, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fixturef("%s[%d] must be an object", key, i)
		}
		for _, r := range required {
			if _, has := obj[r]; !has {
				return nil, fixturef("%s[%d] has no %s", key, i, r)
			}
		}
		if allowed != nil {
			for k := range obj {
				if !slices.Contains(allowed, k) {
					return nil, fixturef("%s[%d] has the unknown property %q", key, i, k)
				}
			}
		}
		if err := targets(fmt.Sprintf("%s[%d].targets", key, i), obj["targets"], version); err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}

// targets are strings in OpenADR 3.1.0 and valuesMap objects in 3.0.1.
func targets(where string, v any, version string) error {
	if v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return fixturef("%s must be a list of distinct targets", where)
	}
	seen := map[string]bool{}
	for _, item := range list {
		key, _ := json.Marshal(item)
		if seen[string(key)] {
			return fixturef("%s must be a list of distinct targets", where)
		}
		seen[string(key)] = true
		if version == "3.0.1" {
			m, ok := item.(map[string]any)
			_, hasType := m["type"]
			values, hasValues := m["values"].([]any)
			if !ok || len(m) != 2 || !hasType || !hasValues {
				return fixturef("%s[] must be an object of type and values (OpenADR 3.0.1)", where)
			}
			if _, err := label(where+"[].type", m["type"]); err != nil {
				return err
			}
			if len(values) == 0 {
				return fixturef("%s[].values must be a non-empty list of strings, numbers or booleans", where)
			}
			for _, x := range values {
				switch x.(type) {
				case string, json.Number, bool:
				default:
					return fixturef("%s[].values must be a non-empty list of strings, numbers or booleans", where)
				}
			}
		} else if _, err := label(where+"[]", item); err != nil {
			return err
		}
	}
	return nil
}

// served refuses a fixture of a revision the adapter does not serve.
func served(f *fixture) error {
	if f.Version != openadrVersion {
		return fixturef("the fixture is of OpenADR %s; this adapter serves %s", f.Version, openadrVersion)
	}
	return nil
}

func isFixtureError(err error) bool {
	var fe fixtureError
	return errors.As(err, &fe)
}
