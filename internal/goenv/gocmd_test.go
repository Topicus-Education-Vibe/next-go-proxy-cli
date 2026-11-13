package goenv

import (
	"context"
	"path/filepath"
	"testing"
)

// isolate points the go command at an empty go env file and clears inherited settings.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GOENV", filepath.Join(t.TempDir(), "go.env"))
	t.Setenv("GOTOOLCHAIN", "local")
	for _, k := range []string{"GOPROXY", "GOAUTH", "GONOSUMDB", "GOPRIVATE", "GONOPROXY", "GOFLAGS"} {
		t.Setenv(k, "")
	}
}

// Runs the real go command: write, read back, merge again without changes.
func TestGoReadWrite(t *testing.T) {
	isolate(t)
	ctx := context.Background()
	g := Go{}
	env, err := g.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if env.GOAUTH != "netrc" || env.GOPROXY != "https://proxy.golang.org,direct" {
		t.Fatalf("unexpected defaults: %+v", env)
	}
	d := Desired{ProxyURL: proxy, Prefixes: prefixes, GoAuthCmd: "ngp goauth"}
	p, err := Merge(env, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Write(ctx, p.Set, nil); err != nil {
		t.Fatal(err)
	}
	env, err = g.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if env.GOAUTH != "netrc;ngp goauth" || env.GOPROXY != proxy+",https://proxy.golang.org,direct" ||
		env.GONOSUMDB != "github.com/topicusonderwijs,github.com/Topicus-Education-Vibe" {
		t.Fatalf("after write: %+v", env)
	}
	if !env.Changed["GOPROXY"] || env.Changed["GOPRIVATE"] {
		t.Errorf("changed = %v", env.Changed)
	}
	if p, _ := Merge(env, d); len(p.Set) != 0 {
		t.Errorf("second merge changes %v", p.Set)
	}

	// An empty value unsets the variable.
	if err := g.Write(ctx, map[string]string{"GONOSUMDB": ""}, nil); err != nil {
		t.Fatal(err)
	}
	if env, _ = g.Read(ctx); env.GONOSUMDB != "" {
		t.Errorf("GONOSUMDB after unset = %q", env.GONOSUMDB)
	}
}

func TestShellOverrides(t *testing.T) {
	isolate(t)
	t.Setenv("GOPROXY", "https://shell.example")
	t.Setenv("GOAUTH", "netrc;ngp goauth") // equal to the new value: no conflict
	got := ShellOverrides(map[string]string{"GOPROXY": "x", "GOAUTH": "netrc;ngp goauth", "GONOSUMDB": "y"})
	if len(got) != 1 || got[0] != "GOPROXY" {
		t.Errorf("got %v", got)
	}
}
