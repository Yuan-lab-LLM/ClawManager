package records

import (
	"context"
	"database/sql"
	"maps"
	"slices"
	"strings"
)

// Trial keeps the rendered bootstrap and the desired revision. Bundle editors,
// revision history and runtime bindings are outside the startup data path.
var trialColumns = map[string]string{
	"users":                        "id username email password_hash role is_active created_at updated_at auth_provider login_alias external_id",
	"user_quotas":                  "id user_id max_instances max_cpu_cores max_memory_gb max_storage_gb max_gpu_count created_at updated_at",
	"instances":                    "id user_id owner name alias description type runtime_type runtime_variant instance_mode cpu_cores memory_gb disk_gb gpu_enabled gpu_type gpu_count os_type os_version image_registry image_tag environment_overrides_json storage_class mount_path openclaw_config_snapshot_id created_at updated_at",
	"openclaw_injection_snapshots": "id instance_id user_id mode rendered_manifest_json rendered_env_json status created_at updated_at",
	"instance_config_revisions":    "id instance_id revision_no content_json checksum status published_by published_at activated_at created_at updated_at",
	"instance_desired_state":       "id instance_id desired_config_revision_id updated_by updated_at created_at",
}

var trialOptionalColumns = map[string]string{"instances": "runtime_security_profile"}

var trialPredicates = map[string]capturePredicate{
	"openclaw_injection_snapshots": {clause: " WHERE EXISTS (SELECT 1 FROM instances i WHERE i.openclaw_config_snapshot_id = openclaw_injection_snapshots.id)"},
	"instance_desired_state":       {clause: " WHERE desired_config_revision_id IS NOT NULL"},
	"instance_config_revisions":    {clause: " WHERE EXISTS (SELECT 1 FROM instance_desired_state d WHERE d.desired_config_revision_id = instance_config_revisions.id AND d.instance_id = instance_config_revisions.instance_id)"},
}

// TrialSourceTables is the table-level grant scope accepted by trial capture.
func TrialSourceTables() []string { return slices.Sorted(maps.Keys(trialColumns)) }

func trialSourceSession(ctx context.Context, conn *sql.Conn, database, uuid string) error {
	var actual, server, role string
	if !identifier.MatchString(database) || uuid == "" ||
		conn.QueryRowContext(ctx, "SELECT DATABASE(), @@server_uuid, CURRENT_ROLE()").Scan(&actual, &server, &role) != nil ||
		actual != database || server != uuid || role != "NONE" {
		return ErrIsolation
	}
	rows, err := conn.QueryContext(ctx, "SHOW GRANTS FOR CURRENT_USER")
	if err != nil {
		return ErrDatabase
	}
	defer rows.Close()
	seen := make(map[string]bool)
	for rows.Next() {
		var grant string
		if rows.Scan(&grant) != nil {
			return ErrDatabase
		}
		if strings.HasPrefix(grant, "GRANT USAGE ON *.* TO ") && !strings.Contains(grant, " WITH GRANT OPTION") {
			continue
		}
		match := businessTableGrant.FindStringSubmatch(grant)
		if len(match) != 3 || match[1] != database || trialColumns[match[2]] == "" || seen[match[2]] {
			return ErrIsolation
		}
		seen[match[2]] = true
	}
	if rows.Err() != nil || len(seen) != len(trialColumns) {
		return ErrIsolation
	}
	return nil
}

func trialCatalog(ctx context.Context, q queryer, check func() error) ([]Table, error) {
	tables, err := catalogGuarded(ctx, q, check)
	if err != nil {
		return nil, err
	}
	selected := make([]Table, 0, len(trialColumns))
	for _, t := range tables {
		projection := trialColumns[t.Name]
		if projection == "" {
			continue
		}
		allowed := strings.Fields(projection)
		optional := " " + trialOptionalColumns[t.Name] + " "
		cols := make([]Column, 0, len(allowed))
		required := 0
		for _, c := range t.Columns {
			if generatedColumn(c.Extra) {
				continue
			}
			if strings.Contains(" "+projection+" ", " "+c.Name+" ") {
				required++
				cols = append(cols, c)
			} else if strings.Contains(optional, " "+c.Name+" ") {
				cols = append(cols, c)
			}
		}
		if t.Engine != "InnoDB" || len(t.PrimaryKey) != 1 || t.PrimaryKey[0] != "id" || required != len(allowed) {
			return nil, ErrSchema
		}
		t.Columns = cols
		t.ForeignKeys = nil
		t.UniqueKeys = nil
		selected = append(selected, t)
	}
	if len(selected) != len(trialColumns) {
		return nil, ErrSchema
	}
	return selected, nil
}

func CheckTrialSource(ctx context.Context, db *sql.DB, database, uuid string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return ErrDatabase
	}
	defer conn.Close()
	if err := trialSourceSession(ctx, conn, database, uuid); err != nil {
		return err
	}
	_, err = trialCatalog(ctx, conn, nil)
	return err
}

func CheckTrialTargetReader(ctx context.Context, db *sql.DB, target DrillTarget) error {
	_, err := CheckTrialTargetAccount(ctx, db, target, true)
	return err
}

// CheckTrialTargetAccount inspects the live identity without changing the target.
func CheckTrialTargetAccount(ctx context.Context, db *sql.DB, target DrillTarget, readOnly bool) (string, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return "", ErrDatabase
	}
	defer conn.Close()
	if err := target.check(ctx, conn); err != nil {
		return "", err
	}
	if readOnly {
		if err := checkSelectOnly(ctx, conn, target.Database); err != nil {
			return "", err
		}
	}
	var account string
	if err := conn.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&account); err != nil || account == "" {
		return "", ErrDatabase
	}
	return account, nil
}

// CheckTrialRestoreSchema checks the minimum target table shape before an
// artifact is selected. Restore still compares column types to that artifact.
func CheckTrialRestoreSchema(ctx context.Context, reader *sql.DB) error {
	rows, err := reader.QueryContext(ctx, `SELECT t.TABLE_NAME, t.ENGINE, c.COLUMN_NAME, c.COLUMN_KEY, c.EXTRA
		FROM information_schema.TABLES t JOIN information_schema.COLUMNS c
		ON c.TABLE_SCHEMA = t.TABLE_SCHEMA AND c.TABLE_NAME = t.TABLE_NAME
		WHERE t.TABLE_SCHEMA = DATABASE() AND t.TABLE_TYPE = 'BASE TABLE'
		AND t.TABLE_NAME IN ('users', 'user_quotas', 'instances')`)
	if err != nil {
		return ErrDatabase
	}
	defer rows.Close()
	columnsByTable := map[string]map[string]bool{
		"users": {}, "user_quotas": {}, "instances": {},
	}
	for rows.Next() {
		var table, engine, column, key, extra string
		if err := rows.Scan(&table, &engine, &column, &key, &extra); err != nil {
			return ErrDatabase
		}
		cols, ok := columnsByTable[table]
		if !ok || engine != "InnoDB" || (column == "id") != (key == "PRI") {
			return ErrSchema
		}
		cols[column] = !generatedColumn(extra)
	}
	if rows.Err() != nil {
		return ErrDatabase
	}
	for table, cols := range columnsByTable {
		required := trialColumns[table]
		if table == "instances" {
			required += " status"
		}
		for _, column := range strings.Fields(required) {
			if !cols[column] {
				return ErrSchema
			}
		}
	}
	return nil
}

func CaptureTrial(ctx context.Context, db *sql.DB, limits Limits, database, uuid string) (Snapshot, error) {
	snapshot, err := captureTrial(ctx, db, limits, database, uuid, trialPredicates)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, ValidateTrial(snapshot)
}

func captureTrial(ctx context.Context, db *sql.DB, limits Limits, database, uuid string, predicates map[string]capturePredicate) (Snapshot, error) {
	return captureRows(ctx, db, limits, func(conn *sql.Conn) error {
		return trialSourceSession(ctx, conn, database, uuid)
	}, trialCatalog, predicates)
}

func ValidateTrial(s Snapshot) error {
	if err := validateSnapshot(s); err != nil {
		return err
	}
	if len(s.Tables) != len(trialColumns) {
		return ErrSchema
	}
	index := make(map[string]map[string]Row)
	for _, t := range s.Tables {
		projection := trialColumns[t.Name]
		if projection == "" ||
			len(t.PrimaryKey) != 1 || t.PrimaryKey[0] != "id" || len(t.ForeignKeys) != 0 || len(t.UniqueKeys) != 0 {
			return ErrSchema
		}
		rows := make(map[string]Row)
		optional := " " + trialOptionalColumns[t.Name] + " "
		required := 0
		for _, c := range t.Columns {
			if generatedColumn(c.Extra) {
				return ErrSchema
			}
			if strings.Contains(" "+projection+" ", " "+c.Name+" ") {
				required++
			} else if !strings.Contains(optional, " "+c.Name+" ") {
				return ErrSchema
			}
		}
		if required != len(strings.Fields(projection)) {
			return ErrSchema
		}
		for _, r := range t.Rows {
			rows[r["id"].String()] = r
		}
		index[t.Name] = rows
	}
	if err := validateTrialRelationships(s); err != nil {
		return err
	}
	snapshots, revisions := make(map[string]bool), make(map[string]bool)
	for _, instance := range index["instances"] {
		ref := instance["openclaw_config_snapshot_id"]
		if ref.Null {
			continue
		}
		snapshot := index["openclaw_injection_snapshots"][ref.String()]
		if snapshot == nil || snapshot["user_id"].String() != instance["user_id"].String() ||
			(!snapshot["instance_id"].Null && snapshot["instance_id"].String() != instance["id"].String()) {
			return ErrData
		}
		snapshots[ref.String()] = true
	}
	for _, desired := range index["instance_desired_state"] {
		ref := desired["desired_config_revision_id"].String()
		revision := index["instance_config_revisions"][ref]
		if revision == nil || revision["instance_id"].String() != desired["instance_id"].String() {
			return ErrData
		}
		revisions[ref] = true
	}
	if len(snapshots) != len(index["openclaw_injection_snapshots"]) || len(revisions) != len(index["instance_config_revisions"]) {
		return ErrData
	}
	return nil
}

// RestoreTrial uses the drill transaction and its verification against a fresh,
// network-isolated target. The caller must establish and maintain that isolation
// before using this mode because the startup config bytes are preserved.
var trialReset = map[string]map[string]Cell{
	"instances": {
		"status": Text("stopped"), "workspace_path": {Null: true}, "runtime_generation": Text("1"), "workspace_usage_bytes": Text("0"),
		"pvc_name": {Null: true}, "pod_name": {Null: true}, "pod_namespace": {Null: true}, "pod_ip": {Null: true},
		"access_url": {Null: true}, "access_token": {Null: true}, "agent_bootstrap_token": {Null: true},
		"runtime_error_message": {Null: true}, "runtime_error_code": {Null: true}, "provisioning_operation_id": {Null: true},
		"started_at": {Null: true}, "stopped_at": {Null: true},
	},
	"instance_desired_state": {"desired_power_state": Text("stopped"), "desired_runtime_action": {Null: true}},
	"openclaw_injection_snapshots": {
		"selected_resource_ids_json": Text("[]"), "resolved_resources_json": Text("[]"),
		"secret_name": {Null: true}, "activated_at": {Null: true},
	},
}

func trialGeneratedColumn(table string, column Column) bool {
	return generatedColumn(column.Extra) &&
		((table == "instances" && column.Name == "owner_normalized") ||
			(table == "users" && column.Name == "local_username_key"))
}
