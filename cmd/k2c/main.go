// Command k2c is the Kernel2Cloud command line.
//
//	k2c forward <address> <local port>   carry local connections to a TCP port
//	k2c version                          print the version
//
// See README.md.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `k2c: the Kernel2Cloud command line

Usage:
  k2c forward [flags] <address> <local port>
        Listen on 127.0.0.1:<local port> and carry each connection to a
        deployment's TCP port. <address> is the port's address as the console
        shows it, e.g. db-chat-acme.apps.kernel2cloud.ai:443. The API key is
        read from K2C_API_KEY.
  k2c version
        Print the version.

Example:
  K2C_API_KEY=k2c_key_... k2c forward db-chat-acme.apps.kernel2cloud.ai:443 5432
  psql -h 127.0.0.1 -p 5432 -U app
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "forward":
		return forwardCmd(ctx, args[1:], stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "k2c", version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "k2c: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
