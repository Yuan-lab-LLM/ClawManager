package ownerrecords

import (
	"context"
	"log"
)

// Presentation excerpts have their own bounded scan. They must not depend on
// post-epoch question accounting or on a new model invocation being present.
// Fingerprints are hints only; restarting a worker safely checks the source again.
func (s *Store) scanRecentQuestions(ctx context.Context, after uint64, fingerprints map[uint64]string) (uint64, error) {
	if s.HistoryRoot == "" {
		return 0, nil
	}
	rows, err := s.Session.SQL().QueryContext(ctx, `SELECT id,COALESCE(workspace_path,''),user_id,LOWER(TRIM(owner)),type,SHA2(CONCAT(COALESCE(workspace_path,''),'|',COALESCE(access_token,'')),256) FROM instances WHERE id>? AND owner IS NOT NULL AND TRIM(owner)<>'' AND LOWER(TRIM(owner)) NOT IN ('factory-reset-staging@clawmanager.local','admin@clawmanager.local') AND instance_mode='lite' AND type IN ('openclaw','hermes','opencode','deepseek-harness') ORDER BY id LIMIT 20`, after)
	if err != nil {
		return after, err
	}
	type item struct {
		identity   Identity
		workspace  string
		generation string
	}
	var items []item
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.identity.Instance, &v.workspace, &v.identity.Scope, &v.identity.Owner, &v.identity.Runtime, &v.generation); err != nil {
			rows.Close()
			return after, err
		}
		v.identity.Mode = "lite"
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return after, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	for _, v := range items {
		if ctx.Err() != nil {
			return after, ctx.Err()
		}
		after = v.identity.Instance
		fingerprint, e := historyFingerprint(ctx, s.HistoryRoot, v.workspace, v.identity)
		if e == nil && fingerprints[after] == fingerprint+v.generation {
			continue
		}
		if e = s.cacheRecentQuestion(ctx, v.identity, v.workspace); e != nil {
			log.Printf("owner recent question deferred: %T", e)
			continue
		}
		if fingerprint != "" {
			if verified, e := historyFingerprint(ctx, s.HistoryRoot, v.workspace, v.identity); e == nil && verified == fingerprint {
				if len(fingerprints) >= 4096 {
					clear(fingerprints)
				}
				fingerprints[after] = fingerprint + v.generation
			}
		}
	}
	return after, nil
}
