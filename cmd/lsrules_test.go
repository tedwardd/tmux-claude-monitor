package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// Little Snitch's documented rule keys. Anything outside this set would be
// silently ignored on import.
var allowedRuleKeys = map[string]bool{
	"process": true, "via": true, "remote-addresses": true, "remote-hosts": true,
	"remote-domains": true, "remote": true, "direction": true, "action": true,
	"priority": true, "disabled": true, "ports": true, "protocol": true, "notes": true,
}

func marshalGroup(t *testing.T, paths []string, strict bool) map[string]any {
	t.Helper()
	data, err := json.Marshal(buildLSGroup(paths, strict))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// The whole point of generating rather than shipping a static file: a rule group
// from this project must never widen access for anything but this binary.
func TestLSGroupNeverGrantsAccessToAnyProcess(t *testing.T) {
	for _, strict := range []bool{false, true} {
		g := buildLSGroup([]string{"/opt/homebrew/bin/claude-monitor"}, strict)
		for _, r := range g.Rules {
			if r.Process == "any" || r.Process == "" {
				t.Errorf("strict=%v: rule grants access to process %q, must name this binary", strict, r.Process)
			}
			if !strings.HasPrefix(r.Process, "/") {
				t.Errorf("strict=%v: process %q is not an absolute path", strict, r.Process)
			}
		}
	}
}

func TestLSGroupCoversEveryPath(t *testing.T) {
	paths := []string{"/opt/homebrew/bin/claude-monitor", "/opt/homebrew/Caskroom/claude-monitor/1.2.3/claude-monitor"}
	g := buildLSGroup(paths, false)

	for _, p := range paths {
		var allowed bool
		for _, r := range g.Rules {
			if r.Action == "allow" && r.Process == p && r.RemoteHosts == usageHost && r.Ports == usagePort {
				allowed = true
			}
		}
		if !allowed {
			t.Errorf("no allow rule for %s", p)
		}
	}
}

func TestLSGroupOnlyAllowsTheUsageEndpoint(t *testing.T) {
	g := buildLSGroup([]string{"/usr/bin/claude-monitor"}, true)
	for _, r := range g.Rules {
		if r.Action != "allow" {
			continue
		}
		if r.RemoteHosts != usageHost {
			t.Errorf("allow rule targets %q, want only %q", r.RemoteHosts, usageHost)
		}
		if r.Ports != usagePort || r.Protocol != "tcp" || r.Direction != "outgoing" {
			t.Errorf("unexpected allow rule scope: %+v", r)
		}
	}
}

// Without --strict the deny rules ship disabled, so importing cannot break a
// working setup; with it they enforce.
func TestLSGroupDenyRulesGatedOnStrict(t *testing.T) {
	for _, tc := range []struct{ strict, wantDisabled bool }{{false, true}, {true, false}} {
		g := buildLSGroup([]string{"/usr/bin/claude-monitor"}, tc.strict)
		var denies int
		for _, r := range g.Rules {
			if r.Action != "deny" {
				continue
			}
			denies++
			if r.Remote != "any" {
				t.Errorf("strict=%v: deny rule remote = %q, want any", tc.strict, r.Remote)
			}
			if r.Disabled != tc.wantDisabled {
				t.Errorf("strict=%v: deny disabled = %v, want %v", tc.strict, r.Disabled, tc.wantDisabled)
			}
		}
		if denies == 0 {
			t.Errorf("strict=%v: no deny rule emitted", tc.strict)
		}
	}
}

func TestLSGroupUsesOnlyDocumentedKeys(t *testing.T) {
	g := marshalGroup(t, []string{"/usr/bin/claude-monitor"}, true)

	for k := range g {
		if k != "name" && k != "description" && k != "rules" {
			t.Errorf("unknown top-level key %q", k)
		}
	}
	rules, ok := g["rules"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatalf("rules missing or empty")
	}
	for _, r := range rules {
		for k := range r.(map[string]any) {
			if !allowedRuleKeys[k] {
				t.Errorf("unknown rule key %q", k)
			}
		}
	}
}

// omitempty must not drop a false Disabled in strict mode in a way that changes
// meaning, and must not emit empty strings for unused remote forms.
func TestLSGroupOmitsUnusedFields(t *testing.T) {
	g := marshalGroup(t, []string{"/usr/bin/claude-monitor"}, true)
	for _, r := range g["rules"].([]any) {
		m := r.(map[string]any)
		for k, v := range m {
			if s, ok := v.(string); ok && s == "" {
				t.Errorf("rule emits empty %q", k)
			}
		}
		if m["action"] == "allow" {
			if _, has := m["remote"]; has {
				t.Error("allow rule should not set remote alongside remote-hosts")
			}
		}
		if m["action"] == "deny" {
			if _, has := m["remote-hosts"]; has {
				t.Error("deny rule should not set remote-hosts alongside remote")
			}
		}
	}
}

func TestExecutablePathsReturnsAtLeastOneAbsolutePath(t *testing.T) {
	paths, err := executablePaths()
	if err != nil {
		t.Fatalf("executablePaths: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no paths returned")
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if !strings.HasPrefix(p, "/") {
			t.Errorf("path %q is not absolute", p)
		}
		if seen[p] {
			t.Errorf("duplicate path %q", p)
		}
		seen[p] = true
	}
}
