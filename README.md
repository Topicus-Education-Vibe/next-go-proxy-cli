# ngp

`ngp` sets up your Go environment for **next-go-proxy**, the Go module proxy for private modules. It logs you in with KeyHub, stores a proxy token, and configures `go` so `go get` of private modules just works.

This repository also holds the API contract (`api/v1`) shared with the next-go-proxy server.

## Install

Requires Go 1.24 or later.

```bash
go install github.com/Topicus-Education-Vibe/next-go-proxy-cli/cmd/ngp@latest
```

Or download a binary from the [releases](https://github.com/Topicus-Education-Vibe/next-go-proxy-cli/releases). Make sure `ngp` is on your `PATH`: the go command runs `ngp goauth` to get credentials.

## Use

```bash
ngp login --server https://<next-go-proxy host>   # once; the host is in your team's docs
go get github.com/<org>/<repo>@latest
```

| Command | What it does |
| --- | --- |
| `ngp login [--server URL] [--device-name NAME]` | Log in with KeyHub (device flow), store a token, configure `go`. The device name (default `$NGP_DEVICE_NAME`, then the hostname) is shown on the proxy's "My tokens" page |
| `ngp update` | Re-apply the server's Go settings, e.g. after a new organization was added |
| `ngp status` | Show login state and token expiry; warn when the Go environment is out of date |
| `ngp logout` | Revoke the token on the server and delete local credentials |
| `ngp goauth` | Credential helper, called by the go command (`GOAUTH`) |

The server URL is remembered; `$NGP_SERVER` works too.

`ngp login` needs no terminal: it prints the KeyHub URL and code on stdout and waits until the code is confirmed or expires. Without a terminal it asks nothing and only warns about `GOPRIVATE`/`GONOPROXY` patterns that bypass the proxy.

## What ngp changes in your Go environment

`ngp` never overwrites your own settings: it merges into the existing values with `go env -w`, and running it twice changes nothing.

| Variable | Change |
| --- | --- |
| `GOPROXY` | Puts the proxy first, keeps the rest (for example `https://proxy.golang.org,direct`) |
| `GOAUTH` | Appends `ngp goauth`, keeps the existing methods (default `netrc`) |
| `GONOSUMDB` | Adds the private module prefixes |
| `GOPRIVATE` / `GONOPROXY` | Unchanged, but `ngp` warns when a pattern makes `go` bypass the proxy and offers to remove it |

A variable set in your shell environment cannot be changed with `go env -w`; `ngp` then prints the value to put in your shell profile.

Credentials are stored in `~/.config/ngp/credentials.json` (mode 0600; `$XDG_CONFIG_HOME` and `$NGP_CONFIG_DIR` are honoured, on Windows `%AppData%\ngp`).

## Development

```bash
go test ./...
golangci-lint run
```

Releases are built by GoReleaser when a `v*` tag is pushed.
