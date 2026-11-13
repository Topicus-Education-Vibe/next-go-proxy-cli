package goenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
)

// MinGoVersion is the first Go release with GOAUTH commands.
const MinGoVersion = "go1.24"

// Go runs the go command to read and write the environment.
type Go struct {
	Path string // go binary; empty means "go" from PATH
}

func (g Go) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := g.Path
	if bin == "" {
		bin = "go"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			return nil, fmt.Errorf("go %s: %s", strings.Join(args, " "), errMsg)
		}
		return nil, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// Read returns the effective Go environment.
func (g Go) Read(ctx context.Context) (Env, error) {
	out, err := g.run(ctx, "env", "-json", "GOPROXY", "GOAUTH", "GONOSUMDB", "GOPRIVATE", "GONOPROXY", "GOVERSION")
	if err != nil {
		return Env{}, err
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		return Env{}, fmt.Errorf("parse go env: %w", err)
	}
	env := Env{
		GOPROXY: m["GOPROXY"], GOAUTH: m["GOAUTH"], GONOSUMDB: m["GONOSUMDB"],
		GOPRIVATE: m["GOPRIVATE"], GONOPROXY: m["GONOPROXY"], GOVERSION: m["GOVERSION"],
		Changed: map[string]bool{},
	}
	if err := CheckVersion(env.GOVERSION); err != nil {
		return env, err
	}
	out, err = g.run(ctx, "env", "-changed", "-json")
	if err != nil {
		return env, err
	}
	var changed map[string]any
	if err := json.Unmarshal(out, &changed); err != nil {
		return env, fmt.Errorf("parse go env -changed: %w", err)
	}
	for k := range changed {
		env.Changed[k] = true
	}
	return env, nil
}

// CheckVersion fails for a Go release older than MinGoVersion; development builds pass.
func CheckVersion(goVersion string) error {
	v, _, _ := strings.Cut(goVersion, " ") // e.g. "go1.24.0 X:foo"
	if !version.IsValid(v) {
		return nil
	}
	if version.Compare(v, MinGoVersion) < 0 {
		return fmt.Errorf("ngp needs Go 1.24 or later for GOAUTH support, found %s; upgrade Go first", v)
	}
	return nil
}

// ShellOverrides returns the variables of set that are fixed by the OS environment;
// 'go env -w' cannot change those, the developer has to change them in their shell profile.
func ShellOverrides(set map[string]string) []string {
	var out []string
	for k, v := range set {
		if cur := os.Getenv(k); cur != "" && cur != v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Write persists the variables with 'go env -w' (or '-u' for an empty value).
// Variables in skip are left alone.
func (g Go) Write(ctx context.Context, set map[string]string, skip []string) error {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var w, u []string
	for _, k := range keys {
		switch {
		case slices.Contains(skip, k):
		case set[k] == "":
			u = append(u, k)
		default:
			w = append(w, k+"="+set[k])
		}
	}
	if len(w) > 0 {
		if _, err := g.run(ctx, append([]string{"env", "-w"}, w...)...); err != nil {
			return err
		}
	}
	if len(u) > 0 {
		if _, err := g.run(ctx, append([]string{"env", "-u"}, u...)...); err != nil {
			return err
		}
	}
	return nil
}
