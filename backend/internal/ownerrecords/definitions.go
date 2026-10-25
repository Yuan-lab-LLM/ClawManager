package ownerrecords

import (
	"context"
	"encoding/json"
)

func (s *Store) ensureDefinitions(ctx context.Context, rule string, tasks []GrowthTask) error {
	s.perf.defsMu.Lock()
	defer s.perf.defsMu.Unlock()
	if s.perf.definitions[rule] {
		return nil
	}
	for _, task := range tasks {
		body, _ := json.Marshal(task)
		if _, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,task_code,definition_version,payload) VALUES ('task_definition',?,0,'active',?,?,?) ON DUPLICATE KEY UPDATE record_key=record_key`, Key("definition", rule, task.Code), task.Code, rule, string(body)); err != nil {
			return err
		}
	}
	if s.perf.definitions == nil {
		s.perf.definitions = map[string]bool{}
	}
	s.perf.definitions[rule] = true
	return nil
}
