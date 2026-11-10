package main

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		diagnostic := runtimeeviction.FailureJSON(err)
		_ = os.WriteFile("/dev/termination-log", diagnostic, 0600)
		fmt.Fprintln(os.Stderr, string(diagnostic))
		os.Exit(1)
	}
}
func run() error {
	var s runtimeeviction.Scope
	if err := json.Unmarshal([]byte(os.Getenv("RECOVERY_SCOPE")), &s); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	client, err := runtimeeviction.DialRuntime(ctx, "/run/containerd/containerd.sock")
	if err != nil {
		return err
	}
	defer client.Close()
	r, err := runtimeeviction.Run(ctx, s, client, s.WorkspaceRoot, "/host-boot-id")
	if err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(b) > 4096 {
		return fmt.Errorf("receipt exceeds termination message limit")
	}
	return os.WriteFile("/dev/termination-log", b, 0600)
}
