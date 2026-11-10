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
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	client, err := runtimeeviction.DialRuntime(ctx, "/run/containerd/containerd.sock")
	if err != nil {
		return err
	}
	defer client.Close()
	b, err := runWithRuntime(ctx, client, "/host-boot-id", os.Getenv("RECOVERY_SCOPE"), os.Getenv("RECOVERY_POD_SCOPE"))
	if err != nil {
		return err
	}
	return os.WriteFile("/dev/termination-log", b, 0600)
}

func runWithRuntime(ctx context.Context, client runtimeeviction.CRIRuntime, bootIDPath, scopeJSON, podScopeJSON string) ([]byte, error) {
	if (scopeJSON == "") == (podScopeJSON == "") {
		return nil, fmt.Errorf("exactly one fixed recovery scope is required")
	}
	var result any
	if podScopeJSON != "" {
		var s runtimeeviction.PodScope
		if err := json.Unmarshal([]byte(podScopeJSON), &s); err != nil {
			return nil, err
		}
		r, err := runtimeeviction.RunPod(ctx, s, client, s.WorkspaceRoot, bootIDPath)
		if err != nil {
			return nil, err
		}
		result = r
	} else {
		var s runtimeeviction.Scope
		if err := json.Unmarshal([]byte(scopeJSON), &s); err != nil {
			return nil, err
		}
		r, err := runtimeeviction.Run(ctx, s, client, s.WorkspaceRoot, bootIDPath)
		if err != nil {
			return nil, err
		}
		result = r
	}
	b, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(b) > 4096 {
		return nil, fmt.Errorf("receipt exceeds termination message limit")
	}
	return b, nil
}
