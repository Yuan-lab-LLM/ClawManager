package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"clawreef/internal/systembackuptrial"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "trial importer accepts configuration through deployment mounts and environment")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := systembackuptrial.New(os.Getenv).RunArtifactImporter(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "trial importer could not serve the registered upload socket")
		os.Exit(1)
	}
}
