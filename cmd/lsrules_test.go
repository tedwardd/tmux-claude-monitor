package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
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
	data, err := json.Marshal(buildLSGroup(localDescription, paths, denyFor(strict)))
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
		g := buildLSGroup(localDescription, []string{"/opt/homebrew/bin/claude-monitor"}, denyFor(strict))
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
	g := buildLSGroup(localDescription, paths, denyDisabled)

	for _, p := range paths {
		var allowed bool
		for _, r := range g.Rules {
			if r.Action == "allow" && r.Process == p && r.RemoteDomains == usageHost && r.Ports == usagePort {
				allowed = true
			}
		}
		if !allowed {
			t.Errorf("no allow rule for %s", p)
		}
	}
}

func TestLSGroupOnlyAllowsTheUsageEndpoint(t *testing.T) {
	g := buildLSGroup(localDescription, []string{"/usr/bin/claude-monitor"}, denyEnabled)
	for _, r := range g.Rules {
		if r.Action != "allow" {
			continue
		}
		if r.RemoteDomains != usageHost {
			t.Errorf("allow rule targets %q, want only %q", r.RemoteDomains, usageHost)
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
		g := buildLSGroup(localDescription, []string{"/usr/bin/claude-monitor"}, denyFor(tc.strict))
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
			if _, has := m["remote-domains"]; has {
				t.Error("deny rule should not set remote-domains alongside remote")
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

func denyFor(strict bool) denyPolicy {
	if strict {
		return denyEnabled
	}
	return denyDisabled
}

// The published group must be usable on any Mac, so it names deterministic
// install paths and never falls back to widening the process.
func TestReleaseGroupCoversBothHomebrewPrefixes(t *testing.T) {
	g := buildLSGroup(releaseDescription("v1.2.3"), releasePaths(), denyOmit)

	want := []string{
		"/opt/homebrew/bin/claude-monitor",
		"/opt/homebrew/Caskroom/claude-monitor/*/claude-monitor",
		"/usr/local/bin/claude-monitor",
		"/usr/local/Caskroom/claude-monitor/*/claude-monitor",
	}
	got := map[string]bool{}
	for _, r := range g.Rules {
		got[r.Process] = true
		if r.Process == "any" {
			t.Fatal("published group must never grant access to any process")
		}
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("published group missing %s", w)
		}
	}
	if len(g.Rules) != len(want) {
		t.Errorf("got %d rules, want %d (allow only, no deny rules)", len(g.Rules), len(want))
	}
}

// The version directory must stay wildcarded, since a concrete version would
// stop matching on the next upgrade, which is the failure this replaced.
func TestReleasePathsWildcardTheVersion(t *testing.T) {
	for _, p := range releasePaths() {
		if strings.Contains(p, "Caskroom") && !strings.Contains(p, "/*/") {
			t.Errorf("Caskroom path is not wildcarded: %s", p)
		}
		for _, digitish := range []string{"0.", "1.", "2."} {
			if strings.Contains(p, digitish) {
				t.Errorf("path embeds a version: %s", p)
			}
		}
	}
}

// A subscription that silently denied traffic would be a bad surprise.
func TestReleaseGroupHasNoDenyRules(t *testing.T) {
	for _, r := range buildLSGroup(releaseDescription("1.0.0"), releasePaths(), denyOmit).Rules {
		if r.Action != "allow" {
			t.Errorf("published group contains a %q rule", r.Action)
		}
	}
}

func TestSubscribeURIIsWellFormed(t *testing.T) {
	uri := subscribeURI()
	if !strings.HasPrefix(uri, "x-littlesnitch:subscribe-rules?url=") {
		t.Errorf("unexpected URI: %s", uri)
	}
	if strings.Contains(uri, " ") {
		t.Errorf("URI contains an unescaped space: %s", uri)
	}
	// The URL must survive escaping intact.
	q := strings.TrimPrefix(uri, "x-littlesnitch:subscribe-rules?url=")
	decoded, err := url.QueryUnescape(q)
	if err != nil {
		t.Fatalf("query unescape: %v", err)
	}
	if decoded != subscriptionURL {
		t.Errorf("round trip gave %q, want %q", decoded, subscriptionURL)
	}
}

// A bare `lsrules` must neither dump the rule group nor touch the firewall.
func TestBareLSRulesNeitherPrintsNorSubscribes(t *testing.T) {
	a := parseLSRulesArgs(nil)
	if a.subscribe {
		t.Error("bare invocation would subscribe")
	}
	if a.printGroup || a.strict || a.release != "" {
		t.Errorf("bare invocation would print a group: %+v", a)
	}
	if len(a.unknown) != 0 {
		t.Errorf("unexpected unknown args: %v", a.unknown)
	}
}

// The explanation stands in for the rule group on a bare run, so it has to say
// what would happen and must not be mistaken for machine output.
func TestLSRulesExplanationExplains(t *testing.T) {
	text := fmt.Sprintf(lsRulesExplanation,
		subscriptionURL, subscriptionURL, usageHost, usagePort, usageHost, usagePort)

	if json.Valid([]byte(text)) {
		t.Error("explanation parses as JSON; the default must not emit a rule group")
	}
	for _, want := range []string{
		"Nothing on your system changes unless you pass --subscribe",
		subscriptionURL,
		usageHost + ":" + usagePort,
		"grants nothing to any other program",
		"--subscribe, --add",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("explanation missing %q", want)
		}
	}
}

func TestLSRulesFlagParsing(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want lsRulesArgs
	}{
		{[]string{"--subscribe"}, lsRulesArgs{subscribe: true}},
		{[]string{"--add"}, lsRulesArgs{subscribe: true}},
		{[]string{"--print"}, lsRulesArgs{printGroup: true}},
		{[]string{"--print", "--strict"}, lsRulesArgs{printGroup: true, strict: true}},
		{[]string{"--release", "v1.2.3"}, lsRulesArgs{release: "v1.2.3"}},
		{[]string{"--release=v1.2.3"}, lsRulesArgs{release: "v1.2.3"}},
	} {
		got := parseLSRulesArgs(tc.args)
		if got.subscribe != tc.want.subscribe || got.printGroup != tc.want.printGroup ||
			got.strict != tc.want.strict || got.release != tc.want.release {
			t.Errorf("parse %v = %+v, want %+v", tc.args, got, tc.want)
		}
		if len(got.unknown) != 0 {
			t.Errorf("parse %v reported unknown args %v", tc.args, got.unknown)
		}
	}

	if got := parseLSRulesArgs([]string{"--bogus"}); len(got.unknown) != 1 {
		t.Errorf("--bogus should be reported as unknown, got %+v", got)
	}
}
