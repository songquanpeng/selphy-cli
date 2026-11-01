package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/songquanpeng/selphy-cli/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Execute(ctx, version, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}
