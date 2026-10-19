package repository

import (
	"clawreef/internal/nbkeys"
	"context"
	"encoding/json"
	"fmt"
)

// Does not change account scopes/enabled or revoke bearer sessions. Comparing
// the policy snapshot also prevents granting against a concurrently stale ceiling.
func (r *NorthboundRepository) UpdateAPIKeys(ctx context.Context, userID int, version int64, enabled bool, scopesJSON string, keys []nbkeys.Key) (bool, error) {
	raw, err := json.Marshal(keys)
	if err != nil {
		return false, err
	}
	if len(keys) > nbkeys.MaxKeys || len(raw) > nbkeys.MaxDocumentBytes {
		return false, fmt.Errorf("API key limit exceeded")
	}
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE northbound_caller_policies SET api_keys_json=?, api_keys_version=api_keys_version+1 WHERE user_id=? AND api_keys_version=? AND enabled=? AND scopes_json=CAST(? AS JSON)`, string(raw), userID, version, enabled, scopesJSON)
	if err != nil {
		return false, fmt.Errorf("failed to save API key configuration")
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
