package services

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"
)

const protectedBrowserURL = "http://clawmanager-local-services.invalid:31282"

// Protected generated config is edited through a pinned directory root. Files
// must be regular, singly linked JSON; symlink/hardlink escapes fail closed.
func patchProtectedBrowserConfig(workspace, workerHost string, enabled bool) error {
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer workspaceRoot.Close()
	root, err := workspaceRoot.OpenRoot("home/.openclaw")
	if err != nil {
		return err
	}
	defer root.Close()
	main, owner, err := readProtectedBrowserJSON(root, "openclaw.json")
	if err != nil {
		return err
	}
	scrubProtectedBrowserURLs(main, workerHost)
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	count := 0
	var generated []string
	for {
		entries, err := directory.ReadDir(100)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			count++
			if count > 4096 {
				return errRuntimeBrowser
			}
			name := entry.Name()
			if !strings.HasPrefix(name, "openclaw.json.bak") && name != "openclaw.json.browser-worker-state" && !strings.HasPrefix(name, ".browser-worker-") {
				continue
			}
			generated = append(generated, name)
		}
		if err == io.EOF {
			break
		}
	}
	for _, name := range generated {
		value, info, err := readProtectedBrowserJSON(root, name)
		if err != nil {
			return err
		}
		scrubProtectedBrowserURLs(value, workerHost)
		if err := writeProtectedBrowserJSON(root, name, value, info); err != nil {
			return err
		}
	}
	if _, err := root.Lstat("openclaw.json.bak-browser-worker"); os.IsNotExist(err) {
		if err := writeProtectedBrowserJSON(root, "openclaw.json.bak-browser-worker", main, owner); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if enabled {
		if _, err := root.Lstat("openclaw.json.browser-worker-state"); os.IsNotExist(err) {
			state := map[string]any{"had_browser": false}
			if browser, present := main["browser"]; present {
				state["had_browser"], state["browser"] = true, browser
			}
			if err := writeProtectedBrowserJSON(root, "openclaw.json.browser-worker-state", state, owner); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		main["browser"] = map[string]any{"enabled": true, "defaultProfile": "interactive", "ssrfPolicy": map[string]any{"allowedHostnames": []string{"clawmanager-local-services.invalid"}}, "profiles": map[string]any{"interactive": map[string]any{"cdpUrl": protectedBrowserURL, "driver": "openclaw", "attachOnly": true}}}
	} else {
		state, _, err := readProtectedBrowserJSON(root, "openclaw.json.browser-worker-state")
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if state != nil {
			if had, _ := state["had_browser"].(bool); had {
				// Native OpenClaw also blocks link-local addresses for trusted hostnames.
				// Permit our private broker with an exact destination allowlist; discovery
				// retains its authority fence and the kernel/backend enforce its port/path.
				main["browser"] = state["browser"]
			} else {
				delete(main, "browser")
			}
		}
	}
	return writeProtectedBrowserJSON(root, "openclaw.json", main, owner)
}

func readProtectedBrowserJSON(root *os.Root, name string) (map[string]any, os.FileInfo, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|workspaceTextOpenFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || workspaceHasMultipleLinks(info) {
		return nil, nil, errRuntimeBrowser
	}
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, nil, errRuntimeBrowser
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, nil, errRuntimeBrowser
	}
	return value, info, nil
}

func writeProtectedBrowserJSON(root *os.Root, name string, value map[string]any, owner os.FileInfo) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".browser-worker-" + hex.EncodeToString(nonce[:])
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY|workspaceTextOpenFlags, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := preserveWorkspaceTextOwner(file, owner); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return root.Rename(temporary, name)
}

// Match only this instance's managed worker address. User-managed third-party
// credentials and unrelated native settings remain intact.
func scrubProtectedBrowserURLs(value any, workerHost string) {
	replacement := func(raw string) string {
		parsed, err := url.Parse(raw)
		if err == nil && parsed.Host == workerHost && (parsed.Scheme == "http" || parsed.Scheme == "https" || parsed.Scheme == "ws" || parsed.Scheme == "wss") {
			return protectedBrowserURL
		}
		return raw
	}
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if raw, ok := child.(string); ok {
				item[key] = replacement(raw)
				continue
			}
			scrubProtectedBrowserURLs(child, workerHost)
		}
	case []any:
		for index, child := range item {
			if raw, ok := child.(string); ok {
				item[index] = replacement(raw)
				continue
			}
			scrubProtectedBrowserURLs(child, workerHost)
		}
	}
}
