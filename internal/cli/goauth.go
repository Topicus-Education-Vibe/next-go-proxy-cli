package cli

import (
	"fmt"
	"io"

	v1 "github.com/Topicus-Education-Vibe/next-go-proxy-cli/api/v1"
	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/credentials"
)

// goauth implements the GOAUTH command protocol ('go help goauth').
// go calls it without arguments, and after a 4xx again with the URL as argument and the
// response on stdin; both are ignored. No valid token: print nothing, exit 0 ("no credentials").
func (a *App) goauth(args []string) int {
	if len(args) > 0 {
		_, _ = io.Copy(io.Discard, a.Stdin) // go writes the response to stdin; drain it
	}
	c, err := credentials.Load(a.CredPath)
	if err != nil || !c.Valid(a.Now()) || c.ProxyURL == "" || !safeToken(c.Token) {
		return 0 // go hides stderr; the server's 401 body tells the user to run 'ngp login'
	}
	fmt.Fprintf(a.Stdout, "%s\n\nAuthorization: Bearer %s\n\n", c.ProxyURL, c.Token)
	return 0
}

// safeToken rejects a hand-edited token that could break the GOAUTH output format.
func safeToken(t string) bool {
	if !v1.IsUserToken(t) {
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
