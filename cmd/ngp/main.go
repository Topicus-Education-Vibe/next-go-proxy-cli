// Command ngp sets up a developer's Go environment for next-go-proxy.
package main

import (
	"os"

	"github.com/Topicus-Education-Vibe/next-go-proxy-cli/internal/cli"
)

// version is set by GoReleaser with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	os.Exit(cli.Main(version))
}
