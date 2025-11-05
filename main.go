package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tsukinoko-kun/jmod/cmd"
	"github.com/tsukinoko-kun/jmod/meta"
	"github.com/tsukinoko-kun/jmod/statusui"
)

func main() {
	ctx, cancel := context.WithCancelCause(context.Background())
	meta.CancelCause = cancel
	defer cancel(nil)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
		sig := <-c
		cancel(fmt.Errorf("received %s", sig))
	}()

	cmd.Execute(ctx)

	if ctx.Err() != nil {
		statusui.Stop()
		// Get the actual error that caused cancellation
		if cause := context.Cause(ctx); cause != nil {
			statusui.Log(fmt.Sprintf("Error: %v", cause), statusui.LogLevelError)
		} else {
			statusui.Log("context canceled", statusui.LogLevelError)
		}
		os.Exit(1)
	}
}
