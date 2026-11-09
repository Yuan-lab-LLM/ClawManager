package services

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Keep persisted identities intact. Only a known PVC backing and uniform slot
// mount layout permit equivalence across a different number of containers.
func runtimeWorkspaceStorageCompatible(source, target string) bool {
	if source == "" || target == "" {
		return false
	}
	if source == target {
		return true
	}
	type layout struct {
		backing           string
		coordinator, slot [2]string
	}
	parse := func(raw string) (layout, bool) {
		result := layout{}
		var identity []json.RawMessage
		if json.Unmarshal([]byte(raw), &identity) != nil || len(identity) != 2 || json.Unmarshal(identity[0], &result.backing) != nil || !strings.HasPrefix(result.backing, "pvc:") {
			return result, false
		}
		backing := strings.Split(strings.TrimPrefix(result.backing, "pvc:"), ":")
		if len(backing) != 3 || backing[1] == "" || backing[2] == "" {
			return result, false
		}
		claim := strings.Split(backing[0], "/")
		if len(claim) != 2 || claim[0] == "" || claim[1] == "" {
			return result, false
		}
		var mounts []string
		if json.Unmarshal(identity[1], &mounts) != nil {
			return result, false
		}
		seen := map[string]bool{}
		coordinator, slots := false, 0
		for _, rawMount := range mounts {
			var fields []json.RawMessage
			if json.Unmarshal([]byte(rawMount), &fields) != nil || len(fields) != 4 {
				return result, false
			}
			var name, root, subPath string
			var readOnly bool
			if json.Unmarshal(fields[0], &name) != nil || json.Unmarshal(fields[1], &root) != nil || json.Unmarshal(fields[2], &subPath) != nil || json.Unmarshal(fields[3], &readOnly) != nil || readOnly || !strings.HasPrefix(root, "/") || seen[name] {
				return result, false
			}
			seen[name] = true
			semantics := [2]string{root, subPath}
			if name == "runtime" {
				result.coordinator = semantics
				coordinator = true
				continue
			}
			if !strings.HasPrefix(name, "slot-") {
				return result, false
			}
			index, err := strconv.Atoi(strings.TrimPrefix(name, "slot-"))
			if err != nil || index < 0 || name != "slot-"+strconv.Itoa(index) {
				return result, false
			}
			if slots > 0 && result.slot != semantics {
				return result, false
			}
			result.slot = semantics
			slots++
		}
		return result, coordinator && slots > 0
	}
	a, ok := parse(source)
	if !ok {
		return false
	}
	b, ok := parse(target)
	return ok && a == b
}
