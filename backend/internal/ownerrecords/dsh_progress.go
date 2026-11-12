package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
)

type dshProgressKey struct{}
type dshProgress struct {
	previous  map[string]string
	confirmed map[string]string
}

// Acknowledged file fingerprints contain no messages. They are persisted only
// after every returned fact is committed, so a partial failure cannot lose work.
func (s *Store) dshProgress(ctx context.Context, item scanItem) (context.Context, *dshProgress, error) {
	p := &dshProgress{previous: map[string]string{}, confirmed: map[string]string{}}
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT payload FROM northbound_owner_records WHERE record_type='history_files' AND record_key=? AND source_key=? AND owner_normalized=? AND scope_id=?`, Key("dsh-files", strconv.FormatUint(item.identity.Instance, 10)), item.generation, item.identity.Owner, item.identity.Scope)
	if e != nil {
		return ctx, nil, e
	}
	var raw []byte
	e = row.Scan(&raw)
	if e != nil && e != sql.ErrNoRows {
		return ctx, nil, e
	}
	if e == nil {
		if e = json.Unmarshal(raw, &p.previous); e != nil {
			return ctx, nil, e
		}
	}
	return context.WithValue(ctx, dshProgressKey{}, p), p, nil
}
func (s *Store) saveDSHProgress(ctx context.Context, item scanItem, p *dshProgress) error {
	if p == nil || len(p.confirmed) == 0 {
		return nil
	}
	raw, e := json.Marshal(p.confirmed)
	if e != nil {
		return e
	}
	old, _ := json.Marshal(p.previous)
	if string(old) == string(raw) {
		return nil
	}
	_, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,source_key,status,payload) SELECT 'history_files',?,i.user_id,LOWER(TRIM(i.owner)),i.id,?,'confirmed',? FROM instances i WHERE i.id=? AND i.user_id=? AND LOWER(TRIM(i.owner))=? AND SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256)=?`+scanRuntimeClause(ctx, "i")+` ON DUPLICATE KEY UPDATE scope_id=VALUES(scope_id),owner_normalized=VALUES(owner_normalized),source_key=VALUES(source_key),payload=VALUES(payload)`, Key("dsh-files", strconv.FormatUint(item.identity.Instance, 10)), item.generation, string(raw), item.identity.Instance, item.identity.Scope, item.identity.Owner, item.generation)
	return e
}
