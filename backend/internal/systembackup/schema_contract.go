package systembackup

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
)

//go:embed schema.json
var builtinSchemaJSON []byte

// BuiltinSchema is generated only from a fresh embedded-migration database and
// checked in for review. A new executable field cannot be silently accepted by
// the backup reader just because its table name happens to be registered.
func BuiltinSchema() ([]SQLTable, error) {
	var tables []SQLTable
	e := json.Unmarshal(builtinSchemaJSON, &tables)
	if e == nil && len(tables) == 0 {
		e = errors.New("empty schema contract")
	}
	return tables, e
}
func DiscoverSchema(ctx context.Context, q SQLQueryer) ([]SQLTable, error) {
	names, e := DiscoverTables(ctx, q)
	if e != nil {
		return nil, e
	}
	out := []SQLTable{}
	for _, name := range names {
		p, _ := Policy(name)
		if p.Category == "system_backup" {
			continue
		}
		table, e := ReadTableSchema(ctx, q, name)
		if e != nil {
			return nil, e
		}
		out = append(out, table)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func ValidateSchema(ctx context.Context, q SQLQueryer) error {
	expected, e := BuiltinSchema()
	if e != nil {
		return e
	}
	actual, e := DiscoverSchema(ctx, q)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(expected, actual) {
		return errors.New("unsupported_schema")
	}
	var executable int
	if e = q.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()) + (SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE()) + (SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA=DATABASE())").Scan(&executable); e != nil {
		return e
	}
	if executable != 0 {
		return errors.New("unsupported_schema")
	}
	return nil
}
