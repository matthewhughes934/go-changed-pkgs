//go:build !windows

package main

import (
	"context"
	"io"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/matthewhughes/signalctx"
)

func TestApp_ExitsOnSigint(t *testing.T) {
	signal := syscall.SIGINT
	expectedCode := 130
	expectedErr := "interrupted (^C)"
	interruptedCtx, cancel := signalctx.NotifyContext(context.Background(), signal)
	defer cancel()

	require.NoError(t, syscall.Kill(syscall.Getpid(), signal))
	require.Eventually(
		t,
		func() bool { return interruptedCtx.Err() != nil },
		time.Second,
		10*time.Millisecond,
	)
	args := append(progArgs, "--from-ref", "HEAD", "--to-ref", "HEAD")

	app := buildTestApp(io.Discard)
	exit, err := runApp(interruptedCtx, app, args)

	assert.Equal(t, expectedCode, exit)
	assert.EqualError(t, err, expectedErr)
}
