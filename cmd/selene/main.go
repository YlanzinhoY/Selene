package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/selene-linux/selene/internal/achievementserver"
	"github.com/selene-linux/selene/internal/cli"
)

func main() {
	if achievementserver.InternalModeRequested() {
		os.Exit(runAchievementServer())
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}

func runAchievementServer() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := achievementserver.Run(ctx, achievementserver.AddressFromEnvironment()); err != nil {
		fmt.Fprintf(os.Stderr, "selene achievements: %v\n", err)
		return 1
	}
	return 0
}
