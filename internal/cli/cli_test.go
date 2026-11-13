package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/Topicus-Education-Vibe/next-go-proxy-cli/api/v1"
	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/credentials"
	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/goenv"
)

// fake is next-go-proxy and its OIDC provider in one TLS test server.
type fake struct {
	srv       *httptest.Server
	mu        sync.Mutex
	prefixes  []string
	minCLI    string
	issued    []string
	revoked   map[string]bool
	exchange  []v1.ExchangeRequest
	tokenReq  []url.Values // form posted to the token endpoint
	deviceReq []url.Values // form posted to the device authorization endpoint
}

func newFake(t *testing.T) *fake {
	f := &fake{revoked: map[string]bool{}, prefixes: []string{"github.com/acme/"}}
	mux := http.NewServeMux()
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	issuer := f.srv.URL + "/oidc"

	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /oidc/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{
			"issuer":                        issuer,
			"device_authorization_endpoint": issuer + "/device",
			"token_endpoint":                issuer + "/token",
		})
	})
	mux.HandleFunc("POST /oidc/device", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.deviceReq = append(f.deviceReq, r.PostForm)
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{
			"device_code": "dc", "user_code": "ABCD-EFGH", "verification_uri": issuer + "/activate",
			"expires_in": 60, "interval": 1,
		})
	})
	mux.HandleFunc("POST /oidc/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.tokenReq = append(f.tokenReq, r.PostForm)
		n := len(f.tokenReq)
		f.mu.Unlock()
		if n == 1 { // the user has not confirmed yet
			writeJSON(w, 400, map[string]string{"error": "authorization_pending"})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": "the-access-token", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("GET "+v1.PathClientConfig, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, 200, v1.ClientConfig{PublicURL: f.srv.URL, Issuer: issuer, CLIClientID: "ngp-cli", PrivatePrefixes: f.prefixes, MinCLIVersion: f.minCLI})
	})
	mux.HandleFunc("POST "+v1.PathExchange, func(w http.ResponseWriter, r *http.Request) {
		var req v1.ExchangeRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.exchange = append(f.exchange, req)
		if req.AccessToken != "the-access-token" {
			writeJSON(w, 401, v1.Error{Code: v1.ErrInvalidAccessToken, Message: "bad access token"})
			return
		}
		tok := v1.UserTokenPrefix + strings.Repeat("a", 40) + string(rune('a'+len(f.issued)))
		f.issued = append(f.issued, tok)
		writeJSON(w, 200, v1.ExchangeResponse{Token: tok, ExpiresAt: time.Now().Add(720 * time.Hour).UTC().Truncate(time.Second)})
	})
	mux.HandleFunc("POST "+v1.PathLogout, func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.revoked[tok] {
			writeJSON(w, 401, v1.Error{Code: v1.ErrUnauthorized, Message: "revoked"})
			return
		}
		f.revoked[tok] = true
		w.WriteHeader(http.StatusNoContent)
	})
	return f
}

type testApp struct {
	*App
	out, errOut *bytes.Buffer
}

func newApp(t *testing.T, f *fake) *testApp {
	t.Setenv("GOENV", filepath.Join(t.TempDir(), "go.env"))
	t.Setenv("GOTOOLCHAIN", "local")
	for _, k := range []string{"GOPROXY", "GOAUTH", "GONOSUMDB", "GOPRIVATE", "GONOPROXY", "GOFLAGS", "NGP_SERVER"} {
		t.Setenv(k, "")
	}
	ta := &testApp{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	ta.App = &App{
		Version:   "v0.2.0",
		Stdin:     strings.NewReader(""),
		Stdout:    ta.out,
		Stderr:    ta.errOut,
		HTTP:      f.srv.Client(),
		CredPath:  filepath.Join(t.TempDir(), "ngp", "credentials.json"),
		GoAuthCmd: "ngp goauth",
		Now:       time.Now,
	}
	return ta
}

func (ta *testApp) run(t *testing.T, args ...string) int {
	t.Helper()
	ta.out.Reset()
	ta.errOut.Reset()
	return ta.Run(context.Background(), args)
}

func goEnv(t *testing.T, k string) string {
	t.Helper()
	out, err := exec.Command("go", "env", k).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestLoginGoauthStatusLogout(t *testing.T) {
	f := newFake(t)
	a := newApp(t, f)

	if code := a.run(t, "login", "--server", f.srv.URL+"/"); code != 0 {
		t.Fatalf("login exit %d: %s", code, a.errOut)
	}
	if !strings.Contains(a.out.String(), "ABCD-EFGH") {
		t.Errorf("user code not shown: %s", a.out)
	}
	// Public client: client_id in the body, never a client secret; resource = public_url on both requests.
	for _, form := range append(f.deviceReq, f.tokenReq...) {
		if form.Get("client_id") != "ngp-cli" || form["client_secret"] != nil || form.Get("resource") != f.srv.URL {
			t.Errorf("request form: %v", form)
		}
	}
	if got := f.deviceReq[0].Get("scope"); got != "openid profile email" {
		t.Errorf("scope = %q", got)
	}
	if len(f.exchange) != 1 || f.exchange[0].DeviceName == "" {
		t.Fatalf("exchange requests: %+v", f.exchange)
	}
	c, err := credentials.Load(a.CredPath)
	if err != nil || c.Token != f.issued[0] || c.Server != f.srv.URL || c.ProxyURL != f.srv.URL {
		t.Fatalf("credentials: %+v, %v", c, err)
	}

	if got, want := goEnv(t, "GOPROXY"), f.srv.URL+",https://proxy.golang.org,direct"; got != want {
		t.Errorf("GOPROXY = %q, want %q", got, want)
	}
	if got := goEnv(t, "GOAUTH"); got != "netrc;ngp goauth" {
		t.Errorf("GOAUTH = %q", got)
	}
	if got := goEnv(t, "GONOSUMDB"); got != "github.com/acme" {
		t.Errorf("GONOSUMDB = %q", got)
	}

	// goauth prints the GOAUTH format, with and without the retry argument.
	want := f.srv.URL + "\n\nAuthorization: Bearer " + f.issued[0] + "\n\n"
	if a.run(t, "goauth"); a.out.String() != want {
		t.Errorf("goauth = %q, want %q", a.out, want)
	}
	a.Stdin = strings.NewReader("HTTP/1.1 401 Unauthorized\n\n")
	if a.run(t, "goauth", f.srv.URL+"/github.com/acme/x/@v/list"); a.out.String() != want {
		t.Errorf("goauth retry = %q", a.out)
	}

	if a.run(t, "status"); !strings.Contains(a.out.String(), "Go env: up to date") {
		t.Errorf("status: %s %s", a.out, a.errOut)
	}

	// A second login is idempotent for go env and revokes the previous token.
	if code := a.run(t, "login"); code != 0 {
		t.Fatalf("second login exit %d: %s", code, a.errOut)
	}
	if !strings.Contains(a.out.String(), "Go environment is up to date") {
		t.Errorf("second login changed go env: %s", a.out)
	}
	if !f.revoked[f.issued[0]] {
		t.Error("previous token not revoked")
	}

	// A new prefix on the server: status warns, update applies it.
	f.mu.Lock()
	f.prefixes = append(f.prefixes, "github.com/acme-two/")
	f.mu.Unlock()
	if a.run(t, "status"); !strings.Contains(a.out.String(), "out of date (GONOSUMDB)") {
		t.Errorf("status after new prefix: %s", a.out)
	}
	if code := a.run(t, "update"); code != 0 {
		t.Fatalf("update exit %d: %s", code, a.errOut)
	}
	if got := goEnv(t, "GONOSUMDB"); got != "github.com/acme,github.com/acme-two" {
		t.Errorf("GONOSUMDB after update = %q", got)
	}

	if code := a.run(t, "logout"); code != 0 {
		t.Fatalf("logout exit %d: %s", code, a.errOut)
	}
	if !f.revoked[f.issued[1]] {
		t.Error("token not revoked on logout")
	}
	if c, _ := credentials.Load(a.CredPath); c != nil {
		t.Error("credentials not deleted")
	}
	if a.run(t, "goauth"); a.out.Len() != 0 {
		t.Errorf("goauth after logout printed %q", a.out)
	}
}

func TestGoauthWithoutValidToken(t *testing.T) {
	f := newFake(t)
	a := newApp(t, f)
	cases := map[string]*credentials.Credentials{
		"expired":      {Server: f.srv.URL, ProxyURL: f.srv.URL, Token: "ngp_u_abc", ExpiresAt: time.Now().Add(-time.Minute)},
		"broken token": {Server: f.srv.URL, ProxyURL: f.srv.URL, Token: "ngp_u_a\nX-Evil: 1", ExpiresAt: time.Now().Add(time.Hour)},
	}
	for name, c := range cases {
		if err := credentials.Save(a.CredPath, c); err != nil {
			t.Fatal(err)
		}
		if code := a.run(t, "goauth"); code != 0 || a.out.Len() != 0 {
			t.Errorf("%s: exit %d, output %q", name, code, a.out)
		}
	}
}

func TestLoginRefusesOldCLIAndPlainHTTP(t *testing.T) {
	f := newFake(t)
	a := newApp(t, f)
	f.minCLI = "v0.3.0"
	if code := a.run(t, "login", "--server", f.srv.URL); code != 1 || !strings.Contains(a.errOut.String(), "too old") {
		t.Errorf("old CLI: exit %d, %s", code, a.errOut)
	}
	if code := a.run(t, "login", "--server", "http://example.com"); code != 1 || !strings.Contains(a.errOut.String(), "https") {
		t.Errorf("http server: exit %d, %s", code, a.errOut)
	}
	if len(f.exchange) != 0 {
		t.Error("no exchange expected")
	}
}

func TestLogoutKeepsWorkingWhenServerAlreadyRevoked(t *testing.T) {
	f := newFake(t)
	a := newApp(t, f)
	c := &credentials.Credentials{Server: f.srv.URL, ProxyURL: f.srv.URL, Token: "ngp_u_gone", ExpiresAt: time.Now().Add(time.Hour)}
	f.revoked[c.Token] = true
	_ = credentials.Save(a.CredPath, c)
	if code := a.run(t, "logout"); code != 0 || a.errOut.Len() != 0 {
		t.Errorf("exit %d, stderr %s", code, a.errOut)
	}
	if c, _ := credentials.Load(a.CredPath); c != nil {
		t.Error("credentials not deleted")
	}
}

func TestGOPRIVATEConflictInteractive(t *testing.T) {
	f := newFake(t)
	a := newApp(t, f)
	if out, err := exec.Command("go", "env", "-w", "GOPRIVATE=github.com/acme/*,example.com/own").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	a.Interactive = true
	a.Stdin = strings.NewReader("y\n")
	if code := a.run(t, "update", "--server", f.srv.URL); code != 0 {
		t.Fatalf("update exit %d: %s", code, a.errOut)
	}
	if !strings.Contains(a.errOut.String(), "GOPRIVATE: github.com/acme/*") {
		t.Errorf("no conflict warning: %s", a.errOut)
	}
	if got := goEnv(t, "GOPRIVATE"); got != "example.com/own" {
		t.Errorf("GOPRIVATE = %q", got)
	}
	if got := goEnv(t, "GONOSUMDB"); got != "github.com/acme/*,example.com/own" {
		t.Errorf("GONOSUMDB = %q", got)
	}
}

// Login and status refuse a Go older than 1.24; a stub go binary reports go1.23.
func TestRefusesOldGo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	f := newFake(t)
	a := newApp(t, f)
	stub := filepath.Join(t.TempDir(), "go")
	script := "#!/bin/sh\necho '{\"GOVERSION\":\"go1.23.4\",\"GOAUTH\":\"netrc\"}'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a.Go = goenv.Go{Path: stub}
	for _, args := range [][]string{{"login", "--server", f.srv.URL}, {"status"}} {
		if code := a.run(t, args...); code != 1 || !strings.Contains(a.errOut.String(), "Go 1.24 or later") {
			t.Errorf("%s: exit %d, %s", args[0], code, a.errOut)
		}
	}
}
