package runtimeeviction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Diagnostics are fixed public messages. Opaque CRI/filesystem errors may contain
// implementation details or credentials and must never enter admin API output.
var failureMessages = []string{
	"Node runtime recovery failed without a safe diagnostic; inspect helper and node health before Retry.",
	"Writer ledger owner or generation changed; inspect the current binding and preserve the new owner before Retry.",
	"Required trusted writer ledger or lock is missing; verify the original workspace volume and ledger before Retry.",
	"Trusted workspace, ledger or lock permissions are invalid; repair node storage permissions before Retry.",
	"Source container identity or execution state is uncertain; inspect the pinned Pod UID, container and node before Retry.",
	"Source node boot changed; restore verified node identity or isolate the original node before recovery.",
	"Timed out verifying source stop or acquiring its writer lock; inspect node runtime health and the old writer before Retry.",
	"Fixed recovery scope is invalid; refresh the source identity and preflight before Retry.",
}

func FailureJSON(err error) []byte {
	message := failureMessages[0]
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		message = failureMessages[6]
	case os.IsNotExist(err):
		message = failureMessages[2]
	case os.IsPermission(err):
		message = failureMessages[3]
	case err != nil:
		text := err.Error()
		switch {
		case text == "ledger owner or generation changed" || text == "invalid protected writer ledger identity" || text == "invalid protected ledger filename":
			message = failureMessages[1]
		case strings.HasPrefix(text, "untrusted "):
			message = failureMessages[3]
		case text == "node boot identity changed":
			message = failureMessages[5]
		case text == "missing ledger requires positively identified pinned container" || text == "invalid durable stop intent" || text == "container identity mismatch" || text == "sandbox identity mismatch" || text == "container state unknown" || text == "old source still executing" || strings.HasPrefix(text, "CRI "):
			message = failureMessages[4]
		case text == "invalid orphan recovery scope" || text == "incomplete orphan recovery identity or terminal barrier" || text == "incomplete recovery scope" || text == "noncanonical slot or gateway identity" || text == "unsupported container identity" || text == "invalid recovery identity" || text == "workspace differs from scope":
			message = failureMessages[7]
		}
	}
	b, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{message})
	return b
}
func ParseFailureJSON(b []byte) (string, error) {
	if len(b) > 4096 {
		return "", fmt.Errorf("oversized helper diagnostic")
	}
	var fields map[string]string
	if e := json.Unmarshal(b, &fields); e != nil || len(fields) != 1 {
		return "", fmt.Errorf("invalid helper diagnostic")
	}
	for _, allowed := range failureMessages {
		if fields["error"] == allowed {
			return allowed, nil
		}
	}
	return "", fmt.Errorf("untrusted helper diagnostic")
}
