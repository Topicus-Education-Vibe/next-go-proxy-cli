package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	v1 "github.com/Topicus-Education-Vibe/next-go-proxy-cli/api/v1"
)

// apiClient talks to the next-go-proxy JSON API.
type apiClient struct {
	hc   *http.Client
	base string // server URL, https, no trailing slash
}

// normalizeServer validates a server URL: https, host only or with a path, no query.
func normalizeServer(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid server URL %q, expected https://host", s)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("server URL %q must use https: go only sends credentials over https", s)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("server URL %q must not contain credentials, a query or a fragment", s)
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

func (c *apiClient) do(ctx context.Context, method, path, bearer string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", v1.ContentTypeJSON)
	if in != nil {
		req.Header.Set("Content-Type", v1.ContentTypeJSON)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var apiErr v1.Error
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Code != "" {
			return &httpError{Status: resp.StatusCode, API: &apiErr}
		}
		return &httpError{Status: resp.StatusCode, Body: strings.TrimSpace(string(truncate(data, 200)))}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: invalid response: %w", method, path, err)
	}
	return nil
}

func truncate(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

// httpError is a non-2xx response; API is set when the body is a v1.Error.
type httpError struct {
	Status int
	API    *v1.Error
	Body   string
}

func (e *httpError) Error() string {
	if e.API != nil {
		return fmt.Sprintf("server: %s (HTTP %d)", e.API.Message, e.Status)
	}
	if e.Body != "" {
		return fmt.Sprintf("server: HTTP %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("server: HTTP %d", e.Status)
}

func statusOf(err error) int {
	var he *httpError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

func (c *apiClient) clientConfig(ctx context.Context) (*v1.ClientConfig, error) {
	var cc v1.ClientConfig
	if err := c.do(ctx, http.MethodGet, v1.PathClientConfig, "", nil, &cc); err != nil {
		return nil, fmt.Errorf("fetch client-config from %s: %w", c.base, err)
	}
	pub, err := normalizeServer(cc.PublicURL)
	if err != nil {
		return nil, fmt.Errorf("client-config: public_url: %w", err)
	}
	cc.PublicURL = pub
	if cc.Issuer == "" || cc.CLIClientID == "" {
		return nil, errors.New("client-config: issuer and cli_client_id are required")
	}
	return &cc, nil
}

func (c *apiClient) exchange(ctx context.Context, idToken, device string) (*v1.ExchangeResponse, error) {
	var resp v1.ExchangeResponse
	err := c.do(ctx, http.MethodPost, v1.PathExchange, "", v1.ExchangeRequest{IDToken: idToken, DeviceName: device}, &resp)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	if !v1.IsUserToken(resp.Token) || resp.ExpiresAt.IsZero() {
		return nil, errors.New("token exchange: server returned no valid token")
	}
	return &resp, nil
}

func (c *apiClient) logout(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodPost, v1.PathLogout, token, nil, nil)
}
