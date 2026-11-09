package ownerrecords

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// This is an optimization hint, not an accounting cursor or an excerpt TTL.
// Include SQLite WAL/SHM; never infer unchanged content from DB mtime alone.
func historyFingerprint(ctx context.Context, root, workspace string, i Identity) (string, error) {
	expected := filepath.Join(root, i.Runtime, fmt.Sprintf("user-%d", i.Scope), fmt.Sprintf("instance-%d", i.Instance))
	if !filepath.IsAbs(root) || filepath.Clean(workspace) != expected {
		return "", fmt.Errorf("workspace identity mismatch")
	}
	h := sha256.New()
	fmt.Fprint(h, i.Owner, "|", i.Runtime, "|")
	add := func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkHistoryPath(root, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s:%d:%d:%s\n", path, info.Size(), info.ModTime().UnixNano(), info.Mode())
		return nil
	}
	if i.Runtime == "hermes" || i.Runtime == "opencode" {
		rel := "home/.hermes/state.db"
		if i.Runtime == "opencode" {
			rel = "home/.local/share/opencode/opencode.db"
		}
		path := filepath.Join(expected, filepath.FromSlash(rel))
		if err := add(path); err != nil {
			return "", err
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Lstat(path + suffix); os.IsNotExist(err) {
				fmt.Fprint(h, suffix, ":absent\n")
			} else if err != nil {
				return "", err
			} else if err = add(path + suffix); err != nil {
				return "", err
			}
		}
	} else {
		rel := "home/.openclaw/agents"
		if i.Runtime == "deepseek-harness" {
			rel = "home/.dsh/sessions"
		} else if i.Runtime != "openclaw" {
			return "", fmt.Errorf("unsupported runtime")
		}
		base := filepath.Join(expected, filepath.FromSlash(rel))
		count := 0
		if err := checkHistoryPath(root, base); err != nil {
			return "", err
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			count++
			if count > 2000 {
				return fmt.Errorf("history directory limit exceeded")
			}
			return add(path)
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
