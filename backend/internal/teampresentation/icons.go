// Package teampresentation owns decoration only. It must never participate in
// role compilation, permissions, Runtime identity or the collaboration roster hash.
package teampresentation

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// This same file is imported by the frontend; there is no second icon taxonomy.
//
//go:embed catalog.json
var catalogJSON []byte

type Entry struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Glyph    string   `json:"glyph"`
	Profiles []string `json:"profiles"`
	Aliases  []string `json:"aliases"`
}

var catalog = func() []Entry {
	var entries []Entry
	if err := json.Unmarshal(catalogJSON, &entries); err != nil {
		panic(err)
	}
	return entries
}()

type Input struct {
	Leader                           bool
	Override, ProfileKey, Role, Name string
	Tags                             []string
}

func normalized(s string) string {
	return strings.NewReplacer("_", "-", " ", "-").Replace(strings.ToLower(strings.TrimSpace(s)))
}
func ValidOverride(key string) bool {
	if key == "" {
		return true
	}
	if key == "leader" || key == "user" || key == "system" || key == "unknown" {
		return false
	}
	for _, e := range catalog {
		if e.Key == key {
			return true
		}
	}
	return false
}
func aliasKey(value string) string {
	value = normalized(value)
	if value == "" {
		return ""
	}
	for _, e := range catalog {
		for _, alias := range e.Aliases {
			if normalized(alias) == value {
				return e.Key
			}
		}
	}
	return ""
}
func Resolve(in Input) string {
	if in.Leader {
		return "leader"
	}
	if in.Override != "" && ValidOverride(in.Override) {
		return in.Override
	}
	for _, e := range catalog {
		if e.Key == "leader" {
			continue
		}
		for _, p := range e.Profiles {
			if p == strings.TrimSpace(in.ProfileKey) {
				return e.Key
			}
		}
	}
	if key := aliasKey(in.Role); key != "" {
		return key
	}
	if key := aliasKey(in.Name); key != "" {
		return key
	}
	// Multiple different professions are ambiguous: do not pick the first tag.
	key := ""
	for _, tag := range in.Tags {
		candidate := aliasKey(tag)
		if candidate == "" {
			continue
		}
		if key != "" && key != candidate {
			return "general"
		}
		key = candidate
	}
	if key != "" {
		return key
	}
	return "general"
}
