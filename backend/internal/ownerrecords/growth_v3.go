package ownerrecords

import (
	"context"
	"time"
)

const growthRuleV3 = "growth-v3"

// Immutable rule version: never reinterpret already-issued v2 rewards.
func growthTasksV3() []GrowthTask {
	tasks := []GrowthTask{}
	for _, task := range growthTasksV2() {
		if task.Code == "question-1" {
			continue
		}
		if task.Code == "question-3" {
			task.XP = 20
		}
		if task.Code == "question-5" {
			task.XP = 30
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func (s *Store) growthV3EffectiveAt(ctx context.Context, now time.Time) (time.Time, error) {
	return s.policy(ctx, growthRuleV3, now, true)
}

type TokenRanking struct {
	Rank         *int      `json:"rank"`
	Participants int       `json:"participants"`
	Tokens       uint64    `json:"tokens"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
}

func rankingRefreshDue(snapshot rankingSnapshot, now time.Time) bool {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	hour := now.In(loc).Hour()
	if hour >= 22 || hour < 6 {
		return false
	}
	return snapshot.owners == nil || now.Before(snapshot.at) || now.Sub(snapshot.at) >= 30*time.Minute
}

// Same verified-owner boundary as the portal's usage summary. Return no peer
// identity or peer usage; only self rank among owners with positive collected use.
func (s *Store) TokenRanking(ctx context.Context, owner string, now time.Time) (*TokenRanking, error) {
	if epoch, e := s.analyticsEpoch(ctx); e != nil {
		return nil, e
	} else if epoch != nil {
		owner, e = normalizedOwner(owner)
		if e != nil {
			return nil, e
		}
		return s.sharedTokenRanking(ctx, owner, now)
	}
	owner, err := normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	s.perf.rankMu.Lock()
	defer s.perf.rankMu.Unlock()
	if rankingRefreshDue(s.perf.rank, now) {
		rows, err := s.factRows(ctx, `SELECT owner_normalized,SUM(total_tokens),RANK() OVER (ORDER BY SUM(total_tokens) DESC) FROM northbound_owner_records WHERE record_type='invocation' AND owner_normalized<>'' AND occurred_at>=? AND occurred_at<? GROUP BY owner_normalized HAVING SUM(total_tokens)>0`, now.Add(-168*time.Hour).UTC(), now.UTC())
		if err != nil {
			return nil, err
		}
		entries := map[string]TokenRanking{}
		for rows.Next() {
			var who string
			var tokens uint64
			var rank int
			if err = rows.Scan(&who, &tokens, &rank); err != nil {
				rows.Close()
				return nil, err
			}
			entries[who] = TokenRanking{Rank: &rank, Tokens: tokens, Start: now.Add(-168 * time.Hour), End: now}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		s.perf.rank = rankingSnapshot{now, entries}
		s.perf.rankBuilds++
	}
	if s.perf.rank.owners == nil {
		return nil, nil
	}
	result := s.perf.rank.owners[owner]
	result.Participants = len(s.perf.rank.owners)
	result.Start = s.perf.rank.at.Add(-168 * time.Hour)
	result.End = s.perf.rank.at
	if result.Rank != nil {
		rank := *result.Rank
		result.Rank = &rank
	}
	return &result, nil
}
