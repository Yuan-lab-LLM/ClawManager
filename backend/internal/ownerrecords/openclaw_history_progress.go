package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
)

type openClawHistoryProgressKey struct{}
type openClawColdAcknowledgement struct {
	Latest     NativeQuestion `json:"latest"`
	AgentKey   string         `json:"agent_key"`
	Session    string         `json:"session"`
	Generation string         `json:"generation"`
	SHA256     string         `json:"sha256"`
}
type openClawColdProgress struct {
	// Only sanitized <=120-rune latest excerpts and immutable identifiers,
	// never complete transcript/assistant content or compressed archive blobs.
	previous  map[string]openClawColdAcknowledgement
	confirmed map[string]openClawColdAcknowledgement
}

func (s *Store) openClawColdProgress(ctx context.Context, item scanItem) (context.Context, *openClawColdProgress, error) {
	p := &openClawColdProgress{previous: map[string]openClawColdAcknowledgement{}, confirmed: map[string]openClawColdAcknowledgement{}}
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT payload FROM northbound_owner_records WHERE record_type='history_files' AND record_key=? AND source_key=? AND owner_normalized=? AND scope_id=?`, Key("oc102-cold-v1", strconv.FormatUint(item.identity.Instance, 10)), scanReadGeneration(item), item.identity.Owner, item.identity.Scope)
	if err != nil {
		return ctx, nil, err
	}
	var raw []byte
	if err = row.Scan(&raw); err != nil && err != sql.ErrNoRows {
		return ctx, nil, err
	}
	if err == nil {
		if len(raw) > 1<<20 || json.Unmarshal(raw, &p.previous) != nil || len(p.previous) > 2000 {
			return ctx, nil, errHistoryRestricted
		}
		for _, a := range p.previous {
			q := a.Latest
			if len([]rune(q.Text)) > 121 || q.Text != questionExcerpt(q.Text) || len(a.Session) > 255 || len(a.Generation) > 128 || len(a.AgentKey) != 64 || len(a.SHA256) != 64 {
				return ctx, nil, errHistoryRestricted
			}
		}
	}
	return context.WithValue(ctx, openClawHistoryProgressKey{}, p), p, nil
}

// Called only after all returned facts have committed. A failed Apply/read or
// ownership change cannot acknowledge unseen questions. No new physical table.
func (s *Store) saveOpenClawColdProgress(ctx context.Context, item scanItem, p *openClawColdProgress) error {
	if p == nil || len(p.confirmed) == 0 {
		return nil
	}
	if len(p.confirmed) > 2000 {
		return errHistoryRestricted
	}
	raw, err := json.Marshal(p.confirmed)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errHistoryRestricted
	}
	old, _ := json.Marshal(p.previous)
	if string(old) == string(raw) {
		return nil
	}
	_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,source_key,status,payload) SELECT 'history_files',?,i.user_id,LOWER(TRIM(i.owner)),i.id,?,'confirmed',? FROM instances i WHERE i.id=? AND i.user_id=? AND LOWER(TRIM(i.owner))=? AND SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256)=?`+scanRuntimeClause(ctx, "i")+` ON DUPLICATE KEY UPDATE scope_id=VALUES(scope_id),owner_normalized=VALUES(owner_normalized),source_key=VALUES(source_key),payload=VALUES(payload)`, Key("oc102-cold-v1", strconv.FormatUint(item.identity.Instance, 10)), scanReadGeneration(item), string(raw), item.identity.Instance, item.identity.Scope, item.identity.Owner, item.generation)
	return err
}
