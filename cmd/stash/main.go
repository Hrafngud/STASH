package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/zalmo/stash/internal/app"
	"github.com/zalmo/stash/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, nil))
}

func run(ctx context.Context, args []string, input io.Reader, output, diagnostics io.Writer, runner *app.Runner) int {
	if cli.IsHelpRequest(args) {
		return finish(ctx, diagnostics, cli.WriteHelp(output))
	}
	if runner == nil {
		var err error
		runner, err = app.NewDefault(ctx, input)
		if err != nil {
			return finish(ctx, diagnostics, err)
		}
	}
	return finish(ctx, diagnostics, runner.Run(ctx, args, output, diagnostics))
}

func finish(ctx context.Context, diagnostics io.Writer, err error) int {
	if err == nil {
		return 0
	}
	if ctx != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return 0
	}
	if diagnostics != nil {
		_, _ = fmt.Fprintf(diagnostics, "stash: %v\n", err)
	}
	return 1
}
