// Command snellius serves the read-only Snellius UI for Kubernetes Gateway API.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/DuncanDoyle/snellius/internal/cli"
	"github.com/DuncanDoyle/snellius/pkg/app"
)

func main() {
	kubeconfig, o, err := cli.Parse(flag.CommandLine, os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, kubeconfig, o); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}
