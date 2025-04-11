package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/urfave/cli/v2"
	"gitlab.com/matthewhughes/signalctx"

	"github.com/matthewhughes934/go-changed-pkgs/internal/flag"
	"github.com/matthewhughes934/go-changed-pkgs/pkg/changed"
)

const (
	_exitSuccess = 0
	_exitFailure = 1
	// https://tldp.org/LDP/abs/html/exitcodes.html
	_signalExitBase = 128
	_sigIntVal      = 2
)

func main() { //go-cov:skip
	app := buildApp(os.Stdout)
	exitCode, err := runApp(context.Background(), app, os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	os.Exit(exitCode)
}

func runApp(ctx context.Context, app *cli.App, args []string) (int, error) {
	ctx, cancel := signalctx.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	if err := app.RunContext(ctx, args); err != nil {
		return getAppStatus(ctx, err)
	}
	return _exitSuccess, nil
}

func buildApp(out io.Writer) *cli.App {
	var (
		repoDir string
		modDir  string
		fromRef string
		toRef   string
	)

	return &cli.App{
		Name:  "changed-go-packages",
		Usage: "Get the changed Go packages between two commits",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "from-ref",
				Destination: &fromRef,
				Required:    true,
			},
			&cli.StringFlag{
				Name:        "to-ref",
				Destination: &toRef,
				Required:    true,
			},
			&cli.StringFlag{
				Name:        "repo-dir",
				Destination: &repoDir,
				Value:       ".",
				Usage:       "The Git repo to inspect",
			},
			&cli.StringFlag{
				Name:        "mod-dir",
				Destination: &modDir,
				Value:       ".",
				Usage:       "Path to the directory containing go.mod. Used to find local packages",
			},
			flag.NewSlogLevelValueFlag(),
		},
		Action: func(cCtx *cli.Context) error {
			logLvl := cCtx.Value("log-level").(slog.Level) //nolint:errcheck
			logger := slog.New(
				slog.NewTextHandler(
					os.Stderr,
					&slog.HandlerOptions{Level: logLvl},
				),
			)
			return printChangedPackages(cCtx.Context, logger, out, repoDir, modDir, fromRef, toRef)
		},
	}
}

func getAppStatus(ctx context.Context, err error) (int, error) {
	if err := signalctx.FromContext(ctx); err != nil {
		if err.Signal == os.Interrupt {
			return _signalExitBase + _sigIntVal, errors.New("interrupted (^C)")
		}
	}
	return _exitFailure, err
}

func printChangedPackages(
	ctx context.Context,
	logger *slog.Logger,
	out io.Writer,
	repoDir string,
	modDir string,
	fromRef string,
	toRef string,
) error {
	packages, err := changed.GetChangedPackages(
		ctx,
		logger,
		repoDir,
		modDir,
		fromRef,
		toRef,
	)
	if err != nil {
		return fmt.Errorf("getting changed packages: %w", err)
	}

	for _, pkg := range packages {
		fmt.Fprintln(out, pkg)
	}
	return nil
}
