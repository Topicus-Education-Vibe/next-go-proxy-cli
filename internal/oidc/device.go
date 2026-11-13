// Package oidc runs the OAuth 2.0 device authorization grant (RFC 8628) against an OIDC provider.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// Scopes requested in the device flow; email lets the server read it from userinfo.
var Scopes = []string{"openid", "profile", "email"}

// Provider holds the endpoints from the discovery document.
type Provider struct {
	Issuer              string `json:"issuer"`
	DeviceAuthorization string `json:"device_authorization_endpoint"`
	Token               string `json:"token_endpoint"`
}

// Discover fetches <issuer>/.well-known/openid-configuration.
func Discover(ctx context.Context, hc *http.Client, issuer string) (*Provider, error) {
	u := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery %s: %s", u, resp.Status)
	}
	var p Provider
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p); err != nil {
		return nil, fmt.Errorf("OIDC discovery %s: %w", u, err)
	}
	if strings.TrimSuffix(p.Issuer, "/") != strings.TrimSuffix(issuer, "/") {
		return nil, fmt.Errorf("OIDC discovery: issuer %q does not match %q", p.Issuer, issuer)
	}
	if p.DeviceAuthorization == "" || p.Token == "" {
		return nil, errors.New("OIDC discovery: provider does not support the device authorization grant")
	}
	return &p, nil
}

// DeviceLogin runs the device flow for a public client and returns the access token,
// requested for resource (RFC 8707) so KeyHub issues an RFC 9068 JWT with aud = resource.
// prompt is called once with the code the user has to confirm in the browser.
func DeviceLogin(ctx context.Context, hc *http.Client, p *Provider, clientID, resource string, prompt func(*oauth2.DeviceAuthResponse)) (string, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, hc)
	cfg := &oauth2.Config{
		ClientID: clientID,
		Scopes:   Scopes,
		Endpoint: oauth2.Endpoint{
			DeviceAuthURL: p.DeviceAuthorization,
			TokenURL:      p.Token,
			AuthStyle:     oauth2.AuthStyleInParams, // public client: client_id in the body, no secret
		},
	}
	res := oauth2.SetAuthURLParam("resource", resource)
	da, err := cfg.DeviceAuth(ctx, res)
	if err != nil {
		return "", fmt.Errorf("device authorization: %w", err)
	}
	prompt(da)
	tok, err := cfg.DeviceAccessToken(ctx, da, res)
	if err != nil {
		return "", fmt.Errorf("device token: %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("device token: provider returned no access token")
	}
	return tok.AccessToken, nil
}
