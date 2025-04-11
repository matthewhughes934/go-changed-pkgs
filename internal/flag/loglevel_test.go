package flag_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/matthewhughes934/go-changed-pkgs/internal/flag"
)

func TestSlogLevelValue_ValidValues(t *testing.T) {
	for _, tc := range []struct {
		levelArg string
		expected slog.Level
	}{
		{
			"debug",
			slog.LevelDebug,
		},
		{
			"info",
			slog.LevelInfo,
		},
		{
			"warn",
			slog.LevelWarn,
		},
		{
			"error",
			slog.LevelError,
		},
	} {
		flags := []cli.Flag{
			// test both building a flag, and ...
			&cli.GenericFlag{
				Name:  "log-level",
				Value: &flag.SlogLevelValue{},
			},
			// ... our pre-packaged flag
			flag.NewSlogLevelValueFlag(),
		}
		for _, flag := range flags {
			t.Run(tc.levelArg, func(t *testing.T) {
				var level slog.Level
				app := &cli.App{
					Flags: []cli.Flag{flag},
					Action: func(ctx *cli.Context) error {
						level = ctx.Value("log-level").(slog.Level) //nolint:errcheck
						return nil
					},
				}

				err := app.Run([]string{"run", "--log-level", tc.levelArg})

				require.NoError(t, err)
				require.Equal(t, tc.expected, level)
			})
		}
	}
}

func TestSlogLevelValue_Defaulting(t *testing.T) {
	var level slog.Level
	defaultLevel := slog.LevelWarn
	app := &cli.App{
		Flags: []cli.Flag{
			&cli.GenericFlag{
				Name:  "log-level",
				Value: &flag.SlogLevelValue{Level: defaultLevel},
			},
		},
		Action: func(ctx *cli.Context) error {
			level = ctx.Value("log-level").(slog.Level) //nolint:errcheck
			return nil
		},
	}

	err := app.Run([]string{"run"})

	require.NoError(t, err)
	require.Equal(t, defaultLevel, level)
}

func TestSlogLevelValue_InvalidValues(t *testing.T) {
	for _, levelArg := range []string{
		"trace",
		"",
		"InFO",
	} {
		t.Run(levelArg, func(t *testing.T) {
			app := &cli.App{
				// avoid noise when running in verbose mode
				// from the app printing its usage string when it sees an
				// invalid flag
				Writer: io.Discard,
				Flags: []cli.Flag{
					&cli.GenericFlag{
						Name:  "log-level",
						Value: &flag.SlogLevelValue{},
					},
				},
			}

			err := app.Run([]string{"run", "--log-level", levelArg})

			require.ErrorContains(
				t,
				err,
				"invalid level "+levelArg+": must be one of: debug, info, warn, error",
			)
		})
	}
}
