package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	v1 "github.com/Topicus-Education-Vibe/next-go-proxy-cli/api/v1"
	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/credentials"
)

// goauth implements the GOAUTH command protocol ('go help goauth').
// go calls it without arguments, and after a 4xx again with the URL as argument and the
// response on stdin; both are ignored. No valid token: print nothing, exit 0 ("no credentials").
//
// $NGP_API_KEY, when set, wins over a login: it is how CI and other machines without a browser
// authenticate. The proxy URL then comes from the stored credentials or else from $NGP_SERVER.
func (a *App) goauth(args []string) int {
	if len(args) > 0 {
		_, _ = io.Copy(io.Discard, a.Stdin) // go writes the response to stdin; drain it
	}
	c, err := credentials.Load(a.CredPath)
	if key := os.Getenv(envAPIKey); key != "" {
		proxy := strings.TrimSuffix(os.Getenv("NGP_SERVER"), "/")
		if err == nil && c != nil && c.ProxyURL != "" {
			proxy = c.ProxyURL
		}
		if proxy != "" && safeToken(key, v1.IsAPIKey) && !strings.ContainsAny(proxy, " \r\n") {
			fmt.Fprintf(a.Stdout, "%s\n\nAuthorization: Bearer %s\n\n", proxy, key)
		}
		return 0
	}
	if err != nil || !c.Valid(a.Now()) || c.ProxyURL == "" || !safeToken(c.Token, v1.IsUserToken) {
		return 0 // go hides stderr; the server's 401 body tells the user to run 'ngp login'
	}
	fmt.Fprintf(a.Stdout, "%s\n\nAuthorization: Bearer %s\n\n", c.ProxyURL, c.Token)
	return 0
}

// envAPIKey holds an ngp_k_ API key made in the next-go-proxy web UI.
const envAPIKey = "NGP_API_KEY"

// safeToken rejects a hand-edited token or key that could break the GOAUTH output format.
func safeToken(t string, hasPrefix func(string) bool) bool {
	if !hasPrefix(t) {
		return false
	}
	for _, r := range t {
		switch {
		case r == '_', r == '-', r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}
