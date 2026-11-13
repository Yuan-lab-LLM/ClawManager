package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/upper/db/v4"
)

var ErrRuntimeOperationScopeUnknown = errors.New("Runtime operation scope is unknown")

func runtimeScopeType(value string) (string, error) {
	kind := strings.ToLower(strings.TrimSpace(value))
	switch kind {
	case "", "openclaw", "opencode", "hermes", "deepseek-harness":
		return kind, nil
	}
	return "", fmt.Errorf("%w: unsupported type %q", ErrRuntimeOperationScopeUnknown, value)
}

// Historical operations predate runtime_type. Resolve each item's frozen source,
// surviving Pod or binding before deciding that it belongs to another type.
// Missing evidence remains a blocker; never rewrite historical journals.
func RuntimeOperationTypes(ctx context.Context, sess db.Session, request, items string) ([]string, error) {
	types := map[string]bool{}
	resolve := func(raw any) (bool, error) {
		found := false
		addType := func(value string) error {
			kind, err := runtimeScopeType(value)
			if err != nil {
				return err
			}
			if kind != "" {
				types[kind] = true
				found = true
			}
			return nil
		}
		var walk func(any) error
		walk = func(v any) error {
			switch value := v.(type) {
			case map[string]any:
				for key, child := range value {
					if key == "instance_ids" {
						if ids, ok := child.([]any); ok {
							for _, id := range ids {
								if err := walk(map[string]any{"instance_id": id}); err != nil {
									return err
								}
							}
						}
					}
					if key == "runtime_type" {
						if kind, ok := child.(string); ok && strings.TrimSpace(kind) != "" {
							if err := addType(kind); err != nil {
								return err
							}
						}
					}
					query := ""
					switch key {
					case "source_pod_id", "target_pod_id", "runtime_pod_id":
						query = "SELECT runtime_type FROM runtime_pods WHERE id=?"
					case "instance_id":
						query = "SELECT runtime_type FROM instance_runtime_bindings WHERE instance_id=?"
					}
					if id, ok := child.(float64); ok && id > 0 && query != "" {
						var kind string
						row, err := sess.SQL().QueryRowContext(ctx, query, int64(id))
						if err == nil {
							err = row.Scan(&kind)
						}
						if err != nil && err != sql.ErrNoRows {
							return err
						}
						if err == nil && kind != "" {
							if err := addType(kind); err != nil {
								return err
							}
						}
					}
					if err := walk(child); err != nil {
						return err
					}
				}
			case []any:
				for _, child := range value {
					if err := walk(child); err != nil {
						return err
					}
				}
			}
			return nil
		}
		err := walk(raw)
		return found, err
	}
	var req any
	if json.Unmarshal([]byte(request), &req) != nil {
		return nil, fmt.Errorf("%w: invalid request journal", ErrRuntimeOperationScopeUnknown)
	}
	_, err := resolve(req)
	if err != nil {
		return nil, err
	}
	// Only an explicit top-level request type scopes otherwise unresolved items.
	// A surviving sibling binding or source Pod is evidence for that sibling only.
	wholeScope := false
	requestFields, _ := req.(map[string]any)
	if value, ok := requestFields["runtime_type"].(string); ok {
		kind, e := runtimeScopeType(value)
		if e != nil {
			return nil, e
		}
		wholeScope = kind != ""
	}
	provenInstances := map[float64]bool{}
	var entries []any
	if json.Unmarshal([]byte(items), &entries) != nil {
		return nil, fmt.Errorf("%w: invalid items journal", ErrRuntimeOperationScopeUnknown)
	}
	for _, entry := range entries {
		itemKnown, e := resolve(entry)
		if e != nil {
			return nil, e
		}
		if !itemKnown && !wholeScope {
			return nil, fmt.Errorf("%w: item has no Runtime type evidence", ErrRuntimeOperationScopeUnknown)
		}
		if itemKnown {
			if fields, ok := entry.(map[string]any); ok {
				if id, ok := fields["instance_id"].(float64); ok {
					provenInstances[id] = true
				}
			}
		}
	}
	if !wholeScope {
		if ids, ok := requestFields["instance_ids"].([]any); ok {
			for _, id := range ids {
				if number, ok := id.(float64); ok && provenInstances[number] {
					continue
				}
				known, e := resolve(map[string]any{"instance_id": id})
				if e != nil {
					return nil, e
				}
				if !known {
					return nil, fmt.Errorf("%w: request instance %v has no Runtime type evidence", ErrRuntimeOperationScopeUnknown, id)
				}
			}
		}
	}
	if len(types) == 0 {
		return nil, fmt.Errorf("%w: no Runtime type evidence", ErrRuntimeOperationScopeUnknown)
	}
	result := make([]string, 0, len(types))
	for kind := range types {
		result = append(result, kind)
	}
	sort.Strings(result)
	return result, nil
}

func checkRuntimeOperationScopes(ctx context.Context, sess db.Session, query string, args []any, runtimeType string) error {
	runtimeType, err := runtimeScopeType(runtimeType)
	if err != nil {
		return err
	}
	rows, err := sess.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	// Close the cursor before resolving Pod/binding evidence on the same SQL tx.
	type operation struct{ id, request, items string }
	var operations []operation
	for rows.Next() {
		var op operation
		if err = rows.Scan(&op.id, &op.request, &op.items); err != nil {
			rows.Close()
			return err
		}
		operations = append(operations, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, op := range operations {
		types, e := RuntimeOperationTypes(ctx, sess, op.request, op.items)
		if e != nil {
			return fmt.Errorf("Runtime operation %s: %w", op.id, e)
		}
		for _, kind := range types {
			if runtimeType == "" || kind == runtimeType {
				return fmt.Errorf("Runtime %s operation %s is active or reserved", kind, op.id)
			}
		}
	}
	return nil
}

func CheckRuntimeManagementOperationsForType(ctx context.Context, sess db.Session, runtimeType, except string, kinds ...string) error {
	query := "SELECT id,request_json,items_json FROM runtime_management_operations WHERE id<>? AND status IN ('pending','processing','starting','needs_attention')"
	args := []any{except}
	if len(kinds) > 0 {
		query += " AND kind IN (" + strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",") + ")"
		for _, kind := range kinds {
			args = append(args, kind)
		}
	}
	return checkRuntimeOperationScopes(ctx, sess, query, args, runtimeType)
}

func CheckRuntimeRolloutActivityForType(ctx context.Context, sess db.Session, runtimeType string) error {
	return checkRuntimeTypeCount(ctx, sess, runtimeType, "SELECT COUNT(*) FROM runtime_rollouts r WHERE r.status IN ('pending','running')")
}

func checkRuntimeTypeCount(ctx context.Context, sess db.Session, runtimeType, query string) error {
	runtimeType, err := runtimeScopeType(runtimeType)
	if err != nil {
		return err
	}
	args := []any{}
	if runtimeType != "" {
		query += " AND (LOWER(TRIM(r.runtime_type))=? OR LOWER(TRIM(r.runtime_type)) NOT IN ('openclaw','opencode','hermes','deepseek-harness') OR r.runtime_type IS NULL)"
		args = append(args, runtimeType)
	}
	var count int
	row, err := sess.SQL().QueryRowContext(ctx, query, args...)
	if err == nil {
		err = row.Scan(&count)
	}
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("Runtime %s upgrade or recovery operation is active", runtimeType)
	}
	return nil
}

// Caller holds the short shared admission transaction lock for publication.
func CheckRuntimeControlAdmission(ctx context.Context, sess db.Session, runtimeType, except string) error {
	for _, query := range []string{
		"SELECT COUNT(*) FROM runtime_rollouts r WHERE r.status IN ('pending','running')",
		"SELECT COUNT(*) FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.recovery_status IN ('pending','starting','retrying','needs_attention') AND ui.state NOT IN ('verified','gateway_verified') AND NOT EXISTS (SELECT 1 FROM runtime_upgrade_items newer JOIN runtime_rollouts nr ON nr.id=newer.rollout_id WHERE newer.instance_id=ui.instance_id AND nr.id>r.id AND nr.status IN ('pending','running','finished','error'))",
	} {
		if err := checkRuntimeTypeCount(ctx, sess, runtimeType, query); err != nil {
			return err
		}
	}
	if err := CheckRuntimeManagementOperationsForType(ctx, sess, runtimeType, except); err != nil {
		return err
	}
	return CheckRuntimePreparationReservationsForType(ctx, sess, except, runtimeType)
}
