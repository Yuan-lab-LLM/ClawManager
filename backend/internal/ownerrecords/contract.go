// Package ownerrecords defines the versioned storage contracts for the owner
// dashboard. It does not activate growth rules or infer human activity from uptime.
package ownerrecords

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
	_ "time/tzdata"
)

// Key uses a JSON array to avoid delimiter and concatenation ambiguities.
// Invocation keys must contain immutable source identity, never mutable owner.
func Key(parts ...string) string {
	body, _ := json.Marshal(parts)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

type TaskTier struct {
	Code     string `json:"tier_code"`
	Target   uint64 `json:"target"`
	RewardXP int64  `json:"reward_xp"`
}

type TaskDefinition struct {
	Code       string     `json:"task_code"`
	Name       string     `json:"name"`
	Period     string     `json:"period_type"`
	Metric     string     `json:"metric"`
	Timezone   string     `json:"timezone"`
	ClaimMode  string     `json:"claim_mode"`
	RewardMode string     `json:"reward_mode"`
	Version    string     `json:"rule_version"`
	Tiers      []TaskTier `json:"tiers"`
}

var codePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (d TaskDefinition) Validate() error {
	if !codePattern.MatchString(d.Code) || !codePattern.MatchString(d.Version) {
		return fmt.Errorf("invalid task or rule identifier")
	}
	if d.Period != "day" && d.Period != "all" {
		return fmt.Errorf("unsupported task period")
	}
	switch d.Metric {
	case "active_work_seconds", "login_days", "effective_questions", "active_streak_days":
	default:
		return fmt.Errorf("unsupported task metric")
	}
	if _, err := time.LoadLocation(d.Timezone); err != nil {
		return fmt.Errorf("invalid task timezone")
	}
	if d.ClaimMode != "automatic" && d.ClaimMode != "manual" {
		return fmt.Errorf("invalid claim mode")
	}
	if d.RewardMode != "incremental" && d.RewardMode != "cumulative" {
		return fmt.Errorf("invalid reward mode")
	}
	if len(d.Tiers) == 0 || len(d.Tiers) > 32 {
		return fmt.Errorf("task must have 1..32 tiers")
	}
	seen := map[string]bool{}
	var previous uint64
	var previousXP int64
	for _, tier := range d.Tiers {
		if !codePattern.MatchString(tier.Code) || seen[tier.Code] || tier.Target <= previous || tier.RewardXP < 0 || tier.RewardXP > 1000000 {
			return fmt.Errorf("invalid task tier")
		}
		if d.RewardMode == "cumulative" && tier.RewardXP < previousXP {
			return fmt.Errorf("cumulative rewards must not decrease")
		}
		seen[tier.Code] = true
		previous = tier.Target
		previousXP = tier.RewardXP
	}
	return nil
}

type Interval struct{ Start, End time.Time }

// UnionSeconds is an accounting primitive, not an activity detector. Callers
// must validate server-confirmed intervals, owner scope and daily boundaries.
func UnionSeconds(intervals []Interval) (uint64, error) {
	if len(intervals) == 0 {
		return 0, nil
	}
	ordered := append([]Interval(nil), intervals...)
	for _, interval := range ordered {
		if interval.Start.IsZero() || interval.End.Before(interval.Start) || interval.End.Sub(interval.Start) > 24*time.Hour {
			return 0, fmt.Errorf("invalid daily usage interval")
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Start.Before(ordered[j].Start) })
	first, last := ordered[0].Start, ordered[0].End
	var total time.Duration
	for _, interval := range ordered[1:] {
		if !interval.Start.After(last) {
			if interval.End.After(last) {
				last = interval.End
			}
			continue
		}
		total += last.Sub(first)
		first, last = interval.Start, interval.End
	}
	total += last.Sub(first)
	return uint64(total / time.Second), nil
}
