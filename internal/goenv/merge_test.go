package goenv

import (
	"maps"
	"slices"
	"testing"
)

const proxy = "https://ngp.example.nl"

var prefixes = []string{"github.com/topicusonderwijs/", "github.com/Topicus-Education-Vibe/"}

func TestMergeGOPROXY(t *testing.T) {
	cases := []struct{ cur, want string }{
		{"https://proxy.golang.org,direct", proxy + ",https://proxy.golang.org,direct"},
		{"https://corp.example/proxy|direct", proxy + ",https://corp.example/proxy|direct"},
		{"direct", proxy + ",direct"},
		{"off", proxy + ",off"},
		{"", proxy + ",https://proxy.golang.org,direct"},
		// already present anywhere, also with a trailing slash: unchanged
		{"https://a.example," + proxy + ",direct", "https://a.example," + proxy + ",direct"},
		{proxy + "/,direct", proxy + "/,direct"},
	}
	for _, tc := range cases {
		if got := MergeGOPROXY(tc.cur, proxy); got != tc.want {
			t.Errorf("MergeGOPROXY(%q) = %q, want %q", tc.cur, got, tc.want)
		}
	}
}

func TestMergeGOAUTH(t *testing.T) {
	cases := []struct{ cur, cmd, want string }{
		{"netrc", "ngp goauth", "netrc;ngp goauth"},
		{"", "ngp goauth", "netrc;ngp goauth"},
		{"git /src;netrc", "ngp goauth", "git /src;netrc;ngp goauth"},
		{"netrc;ngp goauth", "ngp goauth", "netrc;ngp goauth"},
		// binary moved: our own entry is replaced, others stay
		{"netrc;/old/bin/ngp goauth", "ngp goauth", "netrc;ngp goauth"},
		{"netrc;'/Users/a b/ngp' goauth", "ngp goauth", "netrc;ngp goauth"},
		{"netrc", "'/Users/a b/ngp' goauth", "netrc;'/Users/a b/ngp' goauth"},
		// a different tool that happens to have goauth in its name is kept
		{"netrc;mytool goauth", "ngp goauth", "netrc;mytool goauth;ngp goauth"},
	}
	for _, tc := range cases {
		got, err := MergeGOAUTH(tc.cur, tc.cmd)
		if err != nil || got != tc.want {
			t.Errorf("MergeGOAUTH(%q, %q) = %q, %v; want %q", tc.cur, tc.cmd, got, err, tc.want)
		}
	}
	if _, err := MergeGOAUTH("off", "ngp goauth"); err != ErrGoAuthOff {
		t.Errorf("GOAUTH=off: err = %v, want ErrGoAuthOff", err)
	}
}

func TestMergeGONOSUMDB(t *testing.T) {
	cases := []struct{ cur, want string }{
		{"", "github.com/topicusonderwijs,github.com/Topicus-Education-Vibe"},
		{"example.com/x", "example.com/x,github.com/topicusonderwijs,github.com/Topicus-Education-Vibe"},
		// already covered by an org glob, by the org itself, or by everything on github.com
		{"github.com/topicusonderwijs/*", "github.com/topicusonderwijs/*,github.com/Topicus-Education-Vibe"},
		{"github.com/topicusonderwijs,github.com/Topicus-Education-Vibe", "github.com/topicusonderwijs,github.com/Topicus-Education-Vibe"},
		{"github.com", "github.com"},
		// a single repo does not cover the organization
		{"github.com/topicusonderwijs/one", "github.com/topicusonderwijs/one,github.com/topicusonderwijs,github.com/Topicus-Education-Vibe"},
	}
	for _, tc := range cases {
		if got := MergeGONOSUMDB(tc.cur, prefixes); got != tc.want {
			t.Errorf("MergeGONOSUMDB(%q) = %q, want %q", tc.cur, got, tc.want)
		}
	}
}

func TestBypassPatterns(t *testing.T) {
	got := BypassPatterns("example.com/x,github.com/topicusonderwijs/*,github.com/Topicus-Education-Vibe/repo,github.com/other", prefixes)
	want := []string{"github.com/topicusonderwijs/*", "github.com/Topicus-Education-Vibe/repo"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := BypassPatterns("*", prefixes); !slices.Equal(got, []string{"*"}) {
		t.Errorf("'*' must conflict, got %q", got)
	}
}

// The developer's own settings survive, and a second merge changes nothing.
func TestMergeKeepsOwnSettingsAndIsIdempotent(t *testing.T) {
	env := Env{
		GOPROXY:   "https://corp.example/proxy,direct",
		GOAUTH:    "git /src",
		GONOSUMDB: "example.com/private",
		Changed:   map[string]bool{},
	}
	d := Desired{ProxyURL: proxy, Prefixes: prefixes, GoAuthCmd: "ngp goauth"}
	p, err := Merge(env, d)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"GOPROXY":   proxy + ",https://corp.example/proxy,direct",
		"GOAUTH":    "git /src;ngp goauth",
		"GONOSUMDB": "example.com/private,github.com/topicusonderwijs,github.com/Topicus-Education-Vibe",
	}
	if !maps.Equal(p.Set, want) {
		t.Fatalf("first merge: got %v, want %v", p.Set, want)
	}
	env.GOPROXY, env.GOAUTH, env.GONOSUMDB = want["GOPROXY"], want["GOAUTH"], want["GONOSUMDB"]
	p, err = Merge(env, d)
	if err != nil || len(p.Set) != 0 {
		t.Fatalf("second merge must change nothing, got %v, %v", p.Set, err)
	}
}

func TestMergeGOPRIVATEConflict(t *testing.T) {
	// GOPRIVATE set, GONOPROXY and GONOSUMDB follow it (go env reports the inherited value).
	env := Env{
		GOPROXY:   "https://proxy.golang.org,direct",
		GOAUTH:    "netrc",
		GOPRIVATE: "github.com/topicusonderwijs/*,example.com/own",
		GONOPROXY: "github.com/topicusonderwijs/*,example.com/own",
		GONOSUMDB: "github.com/topicusonderwijs/*,example.com/own",
		Changed:   map[string]bool{"GOPRIVATE": true},
	}
	d := Desired{ProxyURL: proxy, Prefixes: prefixes, GoAuthCmd: "ngp goauth"}

	p, _ := Merge(env, d)
	if len(p.Conflicts) != 1 || p.Conflicts[0] != (Conflict{Var: "GOPRIVATE", Pattern: "github.com/topicusonderwijs/*"}) {
		t.Fatalf("conflicts = %v", p.Conflicts)
	}
	if _, ok := p.Set["GOPRIVATE"]; ok {
		t.Fatal("GOPRIVATE must not change without FixPrivate")
	}

	d.FixPrivate = true
	p, _ = Merge(env, d)
	if p.Set["GOPRIVATE"] != "example.com/own" {
		t.Errorf("GOPRIVATE = %q", p.Set["GOPRIVATE"])
	}
	// GONOSUMDB was inherited from GOPRIVATE, so it is pinned with the old patterns plus the prefixes.
	if got, want := p.Set["GONOSUMDB"], "github.com/topicusonderwijs/*,example.com/own,github.com/Topicus-Education-Vibe"; got != want {
		t.Errorf("GONOSUMDB = %q, want %q", got, want)
	}

	// An explicitly set GONOPROXY is the variable to fix.
	env.Changed["GONOPROXY"] = true
	p, _ = Merge(env, d)
	if p.Conflicts[0].Var != "GONOPROXY" || p.Set["GONOPROXY"] != "example.com/own" {
		t.Errorf("explicit GONOPROXY: conflicts %v, set %v", p.Conflicts, p.Set)
	}
}

func TestQuoteCommand(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/local/bin/ngp": "/usr/local/bin/ngp goauth",
		"/Users/a b/ngp":     "'/Users/a b/ngp' goauth",
		"/Users/o'b c/ngp":   `"/Users/o'b c/ngp" goauth`,
	} {
		got := QuoteCommand(in, "goauth")
		if got != want {
			t.Errorf("QuoteCommand(%q) = %q, want %q", in, got, want)
		}
		if w, err := splitWords(got); err != nil || w[0] != in {
			t.Errorf("splitWords(%q) = %q, %v", got, w, err)
		}
	}
}

func TestCheckVersion(t *testing.T) {
	for v, ok := range map[string]bool{
		"go1.24": true, "go1.24.0": true, "go1.27.3": true, "go1.23.9": false, "go1.22": false,
		"devel go1.28-abc": true, "go1.24.0 X:jsonv2": true,
	} {
		if err := CheckVersion(v); (err == nil) != ok {
			t.Errorf("CheckVersion(%q) = %v", v, err)
		}
	}
}
