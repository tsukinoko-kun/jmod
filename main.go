package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/tsukinoko-kun/jmod/cmd"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
		<-c
		cancel()
	}()

	cmd.Execute(ctx)
}
