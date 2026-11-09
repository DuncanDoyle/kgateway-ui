// Command snellius-core serves the Snellius UI with the Gateway API core
// alone: no implementation pack, and an install per GatewayClass controller.
// A private compatibility CI, outside this repo, runs it.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/DuncanDoyle/snellius/internal/cli"
	"github.com/DuncanDoyle/snellius/pkg/app"
	"github.com/DuncanDoyle/snellius/pkg/extension"
)

func main() {
	kubeconfig, o, reg, err := build(os.Args[1:])
	switch {
	case errors.Is(err, flag.ErrHelp):
		os.Exit(0)
	case err != nil:
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.RunWith(ctx, kubeconfig, o, reg); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}

// build parses args with the shared flags and returns the core registry.
// Class-only installs are always on: without a pack they are the only installs.
func build(args []string) (string, app.Options, *extension.Registry, error) {
	kubeconfig, o, err := cli.Parse(flag.NewFlagSet("snellius-core", flag.ContinueOnError), args)
	o.ClassOnlyInstalls = true
	return kubeconfig, o, extension.NewCore(), err
}
