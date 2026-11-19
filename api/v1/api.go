// Package v1 is the JSON contract between the ngp CLI and the next-go-proxy server.
// It holds no hostnames or secrets: everything environment specific comes from client-config.
package v1

import (
	"strings"
	"time"
)

// Paths of the JSON API, relative to the server's public URL.
const (
	PathClientConfig = "/api/v1/client-config" // GET, no authentication
	PathExchange     = "/api/v1/auth/exchange" // POST ExchangeRequest, no authentication (carries a KeyHub access token)
	PathLogout       = "/api/v1/auth/logout"   // POST, Authorization: Bearer ngp_u_..., no body; 204 on success
)

// ContentTypeJSON is the content type of every request and response body of the JSON API.
const ContentTypeJSON = "application/json"

// Token prefixes. Tokens are opaque: prefix + base64url (no padding) of 32 random bytes.
const (
	UserTokenPrefix = "ngp_u_" // issued by next-go-proxy via the exchange, used on laptops
	APIKeyPrefix    = "ngp_k_" // managed in KeyHub, used in CI
)

// IsUserToken reports whether s has the user token prefix.
func IsUserToken(s string) bool { return strings.HasPrefix(s, UserTokenPrefix) }

// IsAPIKey reports whether s has the API key prefix.
func IsAPIKey(s string) bool { return strings.HasPrefix(s, APIKeyPrefix) }

// ClientConfig is the response of GET PathClientConfig.
type ClientConfig struct {
	// PublicURL is the proxy URL clients put in GOPROXY, without trailing slash.
	PublicURL string `json:"public_url"`
	// Issuer is the OIDC issuer; the CLI uses <Issuer>/.well-known/openid-configuration.
	// PublicURL is also the RFC 8707 resource the CLI requests the access token for.
	Issuer string `json:"issuer"`
	// CLIClientID is the public OIDC client used for the device flow.
	CLIClientID string `json:"cli_client_id"`
	// PrivatePrefixes are module path prefixes served by the proxy, e.g. "github.com/topicusonderwijs/".
	PrivatePrefixes []string `json:"private_prefixes"`
	// MinCLIVersion is the lowest ngp version the server supports, semver with "v" (e.g. "v0.1.0"); empty means any.
	MinCLIVersion string `json:"min_cli_version,omitempty"`
}

// MaxDeviceNameLength is the longest DeviceName the server accepts, in bytes.
const MaxDeviceNameLength = 128

// ExchangeRequest is the body of POST PathExchange.
type ExchangeRequest struct {
	// AccessToken is a KeyHub RFC 9068 JWT access token from the device flow, requested
	// with resource = ClientConfig.PublicURL and scope "openid profile email". The server
	// checks typ at+jwt, iss, aud == PublicURL, client_id == CLIClientID and expiry.
	AccessToken string `json:"access_token"`
	// DeviceName identifies the laptop in the token list, e.g. the hostname.
	DeviceName string `json:"device_name"`
}

// ExchangeResponse is the 200 response of POST PathExchange.
type ExchangeResponse struct {
	// Token is the raw ngp_u_ token; the server shows it only this once.
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Error is the JSON body of every non-2xx response of the JSON API.
// GOPROXY endpoints answer with plain text instead, see the messages below.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// ErrorCode is a stable, machine readable error code.
type ErrorCode string

const (
	ErrInvalidRequest     ErrorCode = "invalid_request"      // 400: malformed body or field
	ErrInvalidAccessToken ErrorCode = "invalid_access_token" // 401: exchange with a bad, expired, wrong-audience or wrong-client access token
	ErrUnauthorized       ErrorCode = "unauthorized"         // 401: missing, unknown, expired or revoked token
	ErrForbidden          ErrorCode = "forbidden"            // 403: token type may not perform this action (e.g. ngp_k_ on logout)
	ErrRateLimited        ErrorCode = "rate_limited"         // 429: too many failed authentications
	ErrInternal           ErrorCode = "internal"             // 500
)

// Plain-text 401 bodies of the GOPROXY endpoints; go shows them to the user.
const (
	MsgNoCredentials = "no credentials, run 'ngp login'"
	MsgTokenInvalid  = "token expired or revoked, run 'ngp login'"
)
