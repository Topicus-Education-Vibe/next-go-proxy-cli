// Package goenv merges next-go-proxy settings into a developer's Go environment.
// Rule: never overwrite the developer's own configuration; merging twice changes nothing.
package goenv

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/module"
)

// Env holds the effective values as reported by 'go env'.
type Env struct {
	GOPROXY   string
	GOAUTH    string
	GONOSUMDB string
	GOPRIVATE string
	GONOPROXY string
	GOVERSION string
	// Changed holds the variables that differ from Go's defaults ('go env -changed').
	Changed map[string]bool
}

// Desired is what next-go-proxy needs in the Go environment.
type Desired struct {
	ProxyURL   string   // GOPROXY entry, no trailing slash
	Prefixes   []string // private module path prefixes, e.g. "github.com/org/"
	GoAuthCmd  string   // GOAUTH command that runs 'ngp goauth'
	FixPrivate bool     // remove GOPRIVATE/GONOPROXY patterns that bypass the proxy
}

// Conflict is a pattern that makes go bypass the proxy for a private prefix.
type Conflict struct {
	Var     string // GOPRIVATE or GONOPROXY: the variable that has to change
	Pattern string
}

// Plan is the result of merging: the variables to write and the conflicts found.
type Plan struct {
	Set       map[string]string // variable -> new value; only variables that change
	Conflicts []Conflict
}

// ErrGoAuthOff is returned when GOAUTH=off, which go does not allow to combine with a command.
var ErrGoAuthOff = errors.New("GOAUTH=off disables all authentication; unset it ('go env -u GOAUTH') or add 'ngp goauth' yourself")

// Merge computes the changes needed to bring env in line with d.
func Merge(env Env, d Desired) (Plan, error) {
	p := Plan{Set: map[string]string{}}
	set := func(k, old, v string) {
		if v != old {
			p.Set[k] = v
		}
	}

	set("GOPROXY", env.GOPROXY, MergeGOPROXY(env.GOPROXY, d.ProxyURL))
	auth, err := MergeGOAUTH(env.GOAUTH, d.GoAuthCmd)
	if err != nil {
		return Plan{}, err
	}
	set("GOAUTH", env.GOAUTH, auth)
	set("GONOSUMDB", env.GONOSUMDB, MergeGONOSUMDB(env.GONOSUMDB, d.Prefixes))

	// GONOPROXY defaults to GOPRIVATE, so the fix goes to GOPRIVATE unless GONOPROXY was set explicitly.
	v := "GOPRIVATE"
	if env.Changed["GONOPROXY"] {
		v = "GONOPROXY"
	}
	bad := BypassPatterns(env.GONOPROXY, d.Prefixes)
	for _, pat := range bad {
		p.Conflicts = append(p.Conflicts, Conflict{Var: v, Pattern: pat})
	}
	if d.FixPrivate && len(bad) > 0 {
		old := env.GOPRIVATE
		if v == "GONOPROXY" {
			old = env.GONOPROXY
		}
		set(v, old, strings.Join(removePatterns(splitList(old, ","), bad), ","))
		// GONOSUMDB defaults to GOPRIVATE; pin it so the private prefixes stay out of the checksum database.
		if v == "GOPRIVATE" && !env.Changed["GONOSUMDB"] {
			p.Set["GONOSUMDB"] = MergeGONOSUMDB(env.GONOSUMDB, d.Prefixes)
		}
	}
	return p, nil
}

// MergeGOPROXY puts proxyURL first unless it is already in the list.
func MergeGOPROXY(cur, proxyURL string) string {
	for _, e := range strings.FieldsFunc(cur, func(r rune) bool { return r == ',' || r == '|' }) {
		if strings.TrimSuffix(strings.TrimSpace(e), "/") == proxyURL {
			return cur
		}
	}
	if strings.TrimSpace(cur) == "" {
		return proxyURL + ",https://proxy.golang.org,direct"
	}
	return proxyURL + "," + cur
}

// MergeGOAUTH appends cmd to the existing GOAUTH methods, or replaces an older ngp goauth entry.
func MergeGOAUTH(cur, cmd string) (string, error) {
	cur = strings.TrimSpace(cur)
	if cur == "off" {
		return "", ErrGoAuthOff
	}
	if cur == "" {
		cur = "netrc" // go's default
	}
	entries := splitList(cur, ";")
	for i, e := range entries {
		if isNgpGoAuth(e) {
			if e == cmd {
				return cur, nil
			}
			entries[i] = cmd
			return strings.Join(entries, ";"), nil
		}
	}
	return cur + ";" + cmd, nil
}

func isNgpGoAuth(entry string) bool {
	w, err := splitWords(entry)
	if err != nil || len(w) != 2 || w[1] != "goauth" {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(w[0]), ".exe")
	return base == "ngp"
}

// MergeGONOSUMDB adds every prefix that the existing patterns do not cover yet.
func MergeGONOSUMDB(cur string, prefixes []string) string {
	pats := splitList(cur, ",")
	for _, pre := range prefixes {
		pre = strings.TrimSuffix(pre, "/")
		if pre == "" || covers(strings.Join(pats, ","), pre) {
			continue
		}
		pats = append(pats, pre)
	}
	return strings.Join(pats, ",")
}

// BypassPatterns returns the patterns in globs (GONOPROXY) that make go skip the proxy for a prefix.
func BypassPatterns(globs string, prefixes []string) []string {
	var out []string
	for _, g := range splitList(globs, ",") {
		for _, pre := range prefixes {
			pre = strings.TrimSuffix(pre, "/")
			if covers(g, pre) || strings.HasPrefix(g, pre+"/") {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

// covers reports whether globs match every module under prefix.
// A module path that cannot exist below prefix stands in for "any module there".
func covers(globs, prefix string) bool {
	return module.MatchPrefixPatterns(globs, prefix+"/ngp-probe-module")
}

func removePatterns(list, drop []string) []string {
	return slices.DeleteFunc(list, func(s string) bool { return slices.Contains(drop, s) })
}

func splitList(s, sep string) []string {
	var out []string
	for e := range strings.SplitSeq(s, sep) {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// splitWords splits a GOAUTH command like go does: on spaces, with single or double quotes.
func splitWords(s string) ([]string, error) {
	var words []string
	for {
		s = strings.TrimLeft(s, " \t\n\r")
		if s == "" {
			return words, nil
		}
		if q := s[0]; q == '\'' || q == '"' {
			end := strings.IndexByte(s[1:], q)
			if end < 0 {
				return nil, fmt.Errorf("unterminated %c string", q)
			}
			words = append(words, s[1:1+end])
			s = s[2+end:]
			continue
		}
		end := strings.IndexAny(s, " \t\n\r")
		if end < 0 {
			end = len(s)
		}
		words = append(words, s[:end])
		s = s[end:]
	}
}

// QuoteCommand builds a GOAUTH entry for program with args, quoting the program if needed.
func QuoteCommand(program string, args ...string) string {
	if strings.ContainsAny(program, " \t\"'") {
		if strings.Contains(program, "'") {
			program = `"` + program + `"`
		} else {
			program = "'" + program + "'"
		}
	}
	return strings.Join(append([]string{program}, args...), " ")
}
