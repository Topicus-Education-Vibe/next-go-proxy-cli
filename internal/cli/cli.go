// Package cli implements the ngp commands.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/credentials"
	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/goenv"
	"golang.org/x/mod/semver"
)

// App holds the dependencies of all commands; tests replace them.
type App struct {
	Version     string // semver with "v", or "dev"
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	HTTP        *http.Client
	Go          goenv.Go
	CredPath    string
	GoAuthCmd   string // GOAUTH entry that runs this binary
	Interactive bool   // stdin is a terminal: questions may be asked
	Now         func() time.Time
}

const usage = `ngp sets up your Go environment for next-go-proxy.

Usage:
  ngp login [--server URL]   log in with KeyHub and configure go
  ngp update                 re-apply the server's Go settings, no login needed
  ngp status                 show login state and check the Go environment
  ngp logout                 revoke the token and delete local credentials
  ngp version                print the ngp version
  ngp goauth                 credential helper, called by the go command (GOAUTH)
`

// Main runs ngp with the process environment and returns the exit code.
func Main(version string) int {
	credPath, err := credentials.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ngp:", err)
		return 1
	}
	fi, _ := os.Stdin.Stat()
	a := &App{
		Version:     resolveVersion(version),
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		CredPath:    credPath,
		GoAuthCmd:   selfGoAuthCommand(),
		Interactive: fi != nil && fi.Mode()&os.ModeCharDevice != 0,
		Now:         time.Now,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return a.Run(ctx, os.Args[1:])
}

// Run executes one command and returns the exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.Stderr, usage)
		return 2
	}
	var err error
	switch cmd, rest := args[0], args[1:]; cmd {
	case "goauth":
		return a.goauth(rest) // never fails: go treats a failure as a broken GOAUTH
	case "login":
		err = a.login(ctx, rest)
	case "update":
		err = a.update(ctx, rest)
	case "status":
		err = a.status(ctx, rest)
	case "logout":
		err = a.logout(ctx, rest)
	case "version", "--version", "-v":
		fmt.Fprintln(a.Stdout, "ngp", a.Version)
	case "help", "--help", "-h":
		fmt.Fprint(a.Stdout, usage)
	default:
		fmt.Fprintf(a.Stderr, "ngp: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(a.Stderr, "ngp:", err)
		return 1
	}
	return 0
}

func (a *App) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("ngp "+name, flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	return fs
}

func (a *App) parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return nil
}

// checkMinVersion fails when the server requires a newer ngp; development builds pass.
func (a *App) checkMinVersion(min string) error {
	if min == "" || !semver.IsValid(a.Version) || !semver.IsValid(min) {
		return nil
	}
	if semver.Compare(a.Version, min) < 0 {
		return fmt.Errorf("ngp %s is too old, the server requires %s or later; upgrade: go install github.com/Topicus-Education-Vibe/next-go-proxy-cli/cmd/ngp@latest", a.Version, min)
	}
	return nil
}

func resolveVersion(v string) string {
	if v != "" && v != "dev" {
		return v
	}
	if bi, ok := debug.ReadBuildInfo(); ok && semver.IsValid(bi.Main.Version) {
		return bi.Main.Version // go install ...@vX.Y.Z
	}
	return "dev"
}

// selfGoAuthCommand returns "ngp goauth" when PATH finds this binary, else its absolute path.
func selfGoAuthCommand() string {
	self, err := os.Executable()
	if err != nil {
		return "ngp goauth"
	}
	self, _ = filepath.EvalSymlinks(self)
	if p, err := exec.LookPath("ngp"); err == nil {
		if p, err = filepath.EvalSymlinks(p); err == nil {
			if abs, err := filepath.Abs(p); err == nil && abs == self {
				return "ngp goauth"
			}
		}
	}
	return goenv.QuoteCommand(self, "goauth")
}
