package v1

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// The JSON field names are the wire contract; a rename here breaks deployed clients.
func TestWireFormat(t *testing.T) {
	exp := time.Date(2026, 12, 13, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"client-config", ClientConfig{
			PublicURL: "https://p", Issuer: "https://i", CLIClientID: "c",
			PrivatePrefixes: []string{"github.com/a/"}, MinCLIVersion: "v0.1.0",
		}, `{"public_url":"https://p","issuer":"https://i","cli_client_id":"c","private_prefixes":["github.com/a/"],"min_cli_version":"v0.1.0"}`},
		{"client-config without min version", ClientConfig{PublicURL: "https://p"},
			`{"public_url":"https://p","issuer":"","cli_client_id":"","private_prefixes":null}`},
		{"exchange request", ExchangeRequest{IDToken: "x", DeviceName: "laptop"},
			`{"id_token":"x","device_name":"laptop"}`},
		{"exchange response", ExchangeResponse{Token: "ngp_u_x", ExpiresAt: exp},
			`{"token":"ngp_u_x","expires_at":"2026-12-13T10:00:00Z"}`},
		{"error", Error{Code: ErrUnauthorized, Message: "m"},
			`{"code":"unauthorized","message":"m"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestErrorIsError(t *testing.T) {
	var err error = &Error{Code: ErrInvalidIDToken, Message: "expired"}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != ErrInvalidIDToken {
		t.Fatalf("errors.As failed: %v", err)
	}
	if err.Error() != "invalid_id_token: expired" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestTokenPrefixes(t *testing.T) {
	if !IsUserToken("ngp_u_abc") || IsUserToken("ngp_k_abc") {
		t.Error("IsUserToken")
	}
	if !IsAPIKey("ngp_k_abc") || IsAPIKey("ngp_u_abc") {
		t.Error("IsAPIKey")
	}
}
