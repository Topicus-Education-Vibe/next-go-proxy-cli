package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	v1 "github.com/Topicus-Education-Vibe/next-go-proxy-cli/api/v1"
	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/credentials"
	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/goenv"
	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/oidc"
	"golang.org/x/oauth2"
)

// resolveServer picks the server URL: --server flag, $NGP_SERVER, then the stored credentials.
func resolveServer(flagValue string, c *credentials.Credentials) (string, error) {
	s := flagValue
	if s == "" {
		s = os.Getenv("NGP_SERVER")
	}
	if s == "" && c != nil {
		s = c.Server
	}
	if s == "" {
		return "", errors.New("no server known yet; run 'ngp login --server https://<next-go-proxy host>'")
	}
	return normalizeServer(s)
}

func (a *App) login(ctx context.Context, args []string) error {
	fs := a.flags("login")
	server := fs.String("server", "", "next-go-proxy URL, e.g. https://ngp.example.nl (default: $NGP_SERVER or the last server)")
	device := fs.String("device-name", "", "name of this machine on the proxy's \"My tokens\" page (default: $NGP_DEVICE_NAME or the hostname)")
	if err := a.parse(fs, args); err != nil {
		return err
	}
	name, err := deviceName(*device)
	if err != nil {
		return err
	}
	env, err := a.Go.Read(ctx) // fails early on Go < 1.24
	if err != nil {
		return err
	}
	old, err := credentials.Load(a.CredPath)
	if err != nil {
		old = nil // corrupt file: overwritten below
	}
	srv, err := resolveServer(*server, old)
	if err != nil {
		return err
	}
	api := &apiClient{hc: a.HTTP, base: srv}
	cc, err := api.clientConfig(ctx)
	if err != nil {
		return err
	}
	if err := a.checkMinVersion(cc.MinCLIVersion); err != nil {
		return err
	}
	p, err := oidc.Discover(ctx, a.HTTP, cc.Issuer)
	if err != nil {
		return err
	}
	accessToken, err := oidc.DeviceLogin(ctx, a.HTTP, p, cc.CLIClientID, cc.PublicURL, a.showDeviceCode)
	if err != nil {
		return err
	}
	res, err := api.exchange(ctx, accessToken, name)
	if err != nil {
		return err
	}
	c := &credentials.Credentials{Server: srv, ProxyURL: cc.PublicURL, Token: res.Token, ExpiresAt: res.ExpiresAt}
	if err := credentials.Save(a.CredPath, c); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	// The previous token of this laptop is no longer needed; revoking it is best effort.
	if old.Valid(a.Now()) && old.Server == srv && old.Token != res.Token {
		_ = api.logout(ctx, old.Token)
	}
	fmt.Fprintf(a.Stdout, "Logged in to %s, token valid until %s.\n", srv, res.ExpiresAt.Local().Format(time.DateTime))
	return a.applyGoEnv(ctx, env, cc)
}

func (a *App) showDeviceCode(da *oauth2.DeviceAuthResponse) {
	if da.VerificationURIComplete != "" {
		fmt.Fprintf(a.Stdout, "Open this URL in your browser and confirm code %s:\n\n  %s\n\n", da.UserCode, da.VerificationURIComplete)
	} else {
		fmt.Fprintf(a.Stdout, "Open %s in your browser and enter code:\n\n  %s\n\n", da.VerificationURI, da.UserCode)
	}
	fmt.Fprintln(a.Stdout, "Waiting for confirmation...")
}

// deviceName picks the device name: the --device-name flag, $NGP_DEVICE_NAME, then the hostname.
// A name that is given is checked against the server's rules before the login starts, so a typo
// in a setup script fails at once instead of after the browser step; the hostname is cut to fit.
func deviceName(flagValue string) (string, error) {
	given, from := flagValue, "--device-name"
	if given == "" {
		given, from = os.Getenv("NGP_DEVICE_NAME"), "NGP_DEVICE_NAME"
	}
	if given != "" {
		if !validDeviceName(given) {
			return "", fmt.Errorf("%s: must be 1 to %d bytes of UTF-8 without control characters", from, v1.MaxDeviceNameLength)
		}
		return given, nil
	}
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "unknown", nil
	}
	if len(h) > v1.MaxDeviceNameLength {
		h = strings.ToValidUTF8(h[:v1.MaxDeviceNameLength], "")
	}
	return h, nil
}

// validDeviceName mirrors the server's check: valid UTF-8, not blank, at most MaxDeviceNameLength
// bytes, and no control or format characters (bidi, zero width).
func validDeviceName(s string) bool {
	if len(s) > v1.MaxDeviceNameLength || !utf8.ValidString(s) || strings.TrimSpace(s) == "" {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.In(r, unicode.Cc, unicode.Cf) })
}

func (a *App) update(ctx context.Context, args []string) error {
	fs := a.flags("update")
	server := fs.String("server", "", "next-go-proxy URL (default: $NGP_SERVER or the last server)")
	if err := a.parse(fs, args); err != nil {
		return err
	}
	env, err := a.Go.Read(ctx)
	if err != nil {
		return err
	}
	c, _ := credentials.Load(a.CredPath)
	srv, err := resolveServer(*server, c)
	if err != nil {
		return err
	}
	cc, err := (&apiClient{hc: a.HTTP, base: srv}).clientConfig(ctx)
	if err != nil {
		return err
	}
	if err := a.checkMinVersion(cc.MinCLIVersion); err != nil {
		return err
	}
	if c != nil && c.Server == srv && c.ProxyURL != cc.PublicURL {
		c.ProxyURL = cc.PublicURL
		if err := credentials.Save(a.CredPath, c); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}
	}
	if err := a.applyGoEnv(ctx, env, cc); err != nil {
		return err
	}
	if !c.Valid(a.Now()) {
		fmt.Fprintln(a.Stdout, "You are not logged in; run 'ngp login' before using go.")
	}
	return nil
}

// applyGoEnv merges the client-config into the Go environment and reports what changed.
func (a *App) applyGoEnv(ctx context.Context, env goenv.Env, cc *v1.ClientConfig) error {
	d := goenv.Desired{ProxyURL: cc.PublicURL, Prefixes: cc.PrivatePrefixes, GoAuthCmd: a.GoAuthCmd}
	plan, err := goenv.Merge(env, d)
	if err != nil {
		return err
	}
	if len(plan.Conflicts) > 0 {
		a.warnConflicts(plan.Conflicts)
		if a.Interactive && a.confirm("Remove these patterns so go uses next-go-proxy?") {
			d.FixPrivate = true
			if plan, err = goenv.Merge(env, d); err != nil {
				return err
			}
		}
	}
	if len(plan.Set) == 0 {
		fmt.Fprintln(a.Stdout, "Go environment is up to date.")
		return nil
	}
	shell := goenv.ShellOverrides(plan.Set)
	if err := a.Go.Write(ctx, plan.Set, shell); err != nil {
		return err
	}
	keys := make([]string, 0, len(plan.Set))
	for k := range plan.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if slices.Contains(shell, k) {
			fmt.Fprintf(a.Stderr, "warning: %s is set in your shell environment, so ngp cannot change it.\n  Set it in your shell profile to: %s='%s'\n", k, k, plan.Set[k])
			continue
		}
		fmt.Fprintf(a.Stdout, "go env -w %s=%s\n", k, plan.Set[k])
	}
	return nil
}

func (a *App) warnConflicts(cs []goenv.Conflict) {
	fmt.Fprintln(a.Stderr, "warning: these patterns make go bypass next-go-proxy for private modules:")
	for _, c := range cs {
		fmt.Fprintf(a.Stderr, "  %s: %s\n", c.Var, c.Pattern)
	}
	if !a.Interactive {
		fmt.Fprintln(a.Stderr, "  Remove them with 'go env -w', or run this command in a terminal to let ngp remove them.")
	}
}

func (a *App) confirm(q string) bool {
	fmt.Fprintf(a.Stdout, "%s [y/N] ", q)
	line, _ := bufio.NewReader(a.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "j", "ja":
		return true
	}
	return false
}

func (a *App) status(ctx context.Context, args []string) error {
	fs := a.flags("status")
	if err := a.parse(fs, args); err != nil {
		return err
	}
	env, err := a.Go.Read(ctx)
	if err != nil {
		return err
	}
	c, err := credentials.Load(a.CredPath)
	if err != nil {
		return err
	}
	now := a.Now()
	switch {
	case c == nil || c.Token == "":
		fmt.Fprintln(a.Stdout, "Not logged in; run 'ngp login --server https://<next-go-proxy host>'.")
		return nil
	case !c.Valid(now):
		fmt.Fprintf(a.Stdout, "Server: %s\nToken:  expired on %s; run 'ngp login'.\n", c.Server, c.ExpiresAt.Local().Format(time.DateTime))
	default:
		days := int(c.ExpiresAt.Sub(now).Hours() / 24)
		fmt.Fprintf(a.Stdout, "Server: %s\nToken:  valid until %s (%d days left)\n", c.Server, c.ExpiresAt.Local().Format(time.DateTime), days)
	}
	fmt.Fprintf(a.Stdout, "Go:     %s\n", env.GOVERSION)

	cc, err := (&apiClient{hc: a.HTTP, base: c.Server}).clientConfig(ctx)
	if err != nil {
		fmt.Fprintf(a.Stderr, "warning: cannot check the Go environment: %v\n", err)
		return nil
	}
	if err := a.checkMinVersion(cc.MinCLIVersion); err != nil {
		fmt.Fprintln(a.Stderr, "warning:", err)
	}
	plan, err := goenv.Merge(env, goenv.Desired{ProxyURL: cc.PublicURL, Prefixes: cc.PrivatePrefixes, GoAuthCmd: a.GoAuthCmd})
	if err != nil {
		fmt.Fprintln(a.Stderr, "warning:", err)
		return nil
	}
	if len(plan.Conflicts) > 0 {
		a.warnConflicts(plan.Conflicts)
	}
	if len(plan.Set) == 0 {
		fmt.Fprintln(a.Stdout, "Go env: up to date")
		return nil
	}
	keys := make([]string, 0, len(plan.Set))
	for k := range plan.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(a.Stdout, "Go env: out of date (%s); run 'ngp update'\n", strings.Join(keys, ", "))
	return nil
}

func (a *App) logout(ctx context.Context, args []string) error {
	fs := a.flags("logout")
	if err := a.parse(fs, args); err != nil {
		return err
	}
	c, err := credentials.Load(a.CredPath)
	if err != nil {
		return credentials.Delete(a.CredPath)
	}
	if c == nil {
		fmt.Fprintln(a.Stdout, "Not logged in.")
		return nil
	}
	if c.Valid(a.Now()) {
		err := (&apiClient{hc: a.HTTP, base: c.Server}).logout(ctx, c.Token)
		if err != nil && statusOf(err) != http.StatusUnauthorized {
			fmt.Fprintf(a.Stderr, "warning: could not revoke the token on the server: %v\n  It stays valid until %s; revoke it in the next-go-proxy web UI.\n",
				err, c.ExpiresAt.Local().Format(time.DateTime))
		}
	}
	if err := credentials.Delete(a.CredPath); err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, "Logged out.")
	return nil
}
