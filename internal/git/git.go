package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// A convenience func for running commands.
// Upon success returns the string written from the command's stdout.
// Upton failure returns an error include details from the command's stderr.
func runCmd(cmd *exec.Cmd) (string, error) {
	var stdout strings.Builder
	var stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf(
			"running command: `%s`: %w\nstderr: %s",
			strings.Join(cmd.Args, " "),
			err,
			stderr.String(),
		)
	}

	return stdout.String(), nil
}

func RunGitCmd(ctx context.Context, args ...string) (string, error) {
	return runCmd(exec.CommandContext(ctx, "git", args...))
}
