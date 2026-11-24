package records

import (
	"context"
	"database/sql"
	"strings"
)

var restoreColumns = map[string]string{
	"users":                        "id username email password_hash role is_active created_at updated_at last_login auth_provider login_alias external_id",
	"user_quotas":                  "id user_id max_instances max_cpu_cores max_memory_gb max_storage_gb max_gpu_count created_at updated_at",
	"instances":                    "id user_id owner name alias description type runtime_type runtime_variant instance_mode status cpu_cores memory_gb disk_gb gpu_enabled gpu_type gpu_count os_type os_version image_registry image_tag environment_overrides_json storage_class pvc_name mount_path workspace_path workspace_usage_bytes runtime_generation runtime_error_message provisioning_operation_id pod_name pod_namespace pod_ip access_url access_token agent_bootstrap_token openclaw_config_snapshot_id created_at updated_at started_at stopped_at workbuddy_storage_identity runtime_security_profile workspace_usage_observed_at workspace_usage_epoch workspace_usage_blocked",
	"instance_desired_state":       "id instance_id desired_power_state desired_config_revision_id desired_runtime_action updated_by updated_at created_at",
	"instance_config_revisions":    "id instance_id source_snapshot_id source_bundle_id revision_no content_json checksum status published_by published_at activated_at created_at updated_at",
	"openclaw_injection_snapshots": "id instance_id user_id mode bundle_id selected_resource_ids_json resolved_resources_json rendered_manifest_json rendered_env_json secret_name status error_message created_at updated_at activated_at",
}

func RestoreTrial(ctx context.Context, db *sql.DB, target DrillTarget, source Snapshot, placement Placement) (RestorePlan, error) {
	if db == nil {
		return RestorePlan{}, ErrDatabase
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return RestorePlan{}, ErrDatabase
	}
	defer conn.Close()
	if err := target.check(ctx, conn); err != nil {
		return RestorePlan{}, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RestorePlan{}, ErrDatabase
	}
	defer tx.Rollback()
	catalog, err := Catalog(ctx, tx)
	if err != nil {
		return RestorePlan{}, err
	}
	plan, err := normalizeTrial(ctx, source, catalog, placement)
	if err != nil {
		return RestorePlan{}, err
	}
	plan.trialSeeds, err = captureTrialSeeds(ctx, tx, catalog)
	if err != nil {
		return RestorePlan{}, err
	}
	if err := checkBlankWithSeeds(ctx, tx, catalog, plan.trialSeeds); err != nil {
		return RestorePlan{}, err
	}
	if err := canonicalizeTargetJSON(ctx, tx, &plan); err != nil {
		return RestorePlan{}, err
	}
	if err := target.check(ctx, conn); err != nil {
		return RestorePlan{}, err
	}
	for _, query := range []string{"DELETE FROM user_quotas", "DELETE FROM users"} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return RestorePlan{}, ErrDatabase
		}
	}
	for _, t := range plan.Data.Tables {

		name, _ := quoted(t.Name)
		var cols, marks []string
		for _, c := range t.Columns {
			n, _ := quoted(c.Name)
			cols = append(cols, n)
			marks = append(marks, "?")
		}
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO "+name+" ("+strings.Join(cols, ",")+") VALUES ("+strings.Join(marks, ",")+")")
		if err != nil {
			return RestorePlan{}, ErrDatabase
		}
		for _, r := range t.Rows {
			args := make([]any, len(t.Columns))
			for i, c := range t.Columns {
				v := r[c.Name]
				if !v.Null {
					args[i] = v.Bytes
					// database/sql's MySQL driver encodes []byte(nil) as SQL
					// NULL. Cloning/decoding a non-NULL empty cell may produce
					// nil bytes, but its explicit Null bit remains authoritative.
					if v.Bytes == nil {
						args[i] = []byte{}
					}
				}
			}
			if _, err = stmt.ExecContext(ctx, args...); err != nil {
				stmt.Close()
				return RestorePlan{}, ErrDatabase
			}
		}
		if stmt.Close() != nil {
			return RestorePlan{}, ErrDatabase
		}
	}
	if err := tx.Commit(); err != nil {
		return RestorePlan{}, ErrDatabase
	}
	return plan, nil
}
func normalizeTrial(ctx context.Context, source Snapshot, target []Table, placement Placement) (RestorePlan, error) {
	if err := ValidateTrial(source); err != nil {
		return RestorePlan{}, err
	}
	if placement.Namespace == "" || placement.StorageClass == "" {
		return RestorePlan{}, ErrIsolation
	}
	targets := make(map[string]Table, len(target))
	for _, t := range target {
		if _, repeated := targets[t.Name]; repeated {
			return RestorePlan{}, ErrSchema
		}
		targets[t.Name] = t
	}
	plan := RestorePlan{Data: Snapshot{Version: Version}}
	for _, table := range source.Tables {
		if err := ctx.Err(); err != nil {
			return RestorePlan{}, err
		}
		if len(table.Rows) == 0 {
			continue
		}
		dest, ok := targets[table.Name]
		if !ok || dest.Engine != "InnoDB" || len(dest.PrimaryKey) != 1 || dest.PrimaryKey[0] != "id" {
			return RestorePlan{}, ErrSchema
		}
		out, err := planTrialTable(table, dest, placement)
		if err != nil {
			return RestorePlan{}, err
		}
		switch table.Name {
		case "users":
			plan.Users = len(out.Rows)
		case "instances":
			plan.Instances = len(out.Rows)
		}
		plan.Data.Tables = append(plan.Data.Tables, out)
	}
	ordered, err := sortedTables(plan.Data.Tables)
	if err != nil {
		return RestorePlan{}, err
	}
	plan.Data.Tables = ordered
	return plan, nil
}

func planTrialTable(source, target Table, placement Placement) (Table, error) {
	columns := make(map[string]Column, len(target.Columns))
	allowed := " " + restoreColumns[source.Name] + " "
	reset := trialReset[source.Name]
	out := source
	out.Rows = nil
	out.ForeignKeys = target.ForeignKeys
	out.Columns = append([]Column(nil), source.Columns...)
	for _, c := range target.Columns {
		columns[c.Name] = c
		if trialGeneratedColumn(source.Name, c) {
			continue
		}
		if !strings.Contains(allowed, " "+c.Name+" ") && !(source.Name == "instances" && c.Name == "runtime_error_code") {
			return Table{}, ErrSchema
		}
		if hasColumn(source, c.Name) {
			continue
		}
		if _, shouldReset := reset[c.Name]; shouldReset {
			out.Columns = append(out.Columns, c)
			continue
		}
		if !c.Nullable && c.Default == nil {
			return Table{}, ErrSchema
		}
	}
	for _, c := range source.Columns {
		match, ok := columns[c.Name]
		if !ok || c.Type != match.Type || generatedColumn(match.Extra) {
			return Table{}, ErrSchema
		}
	}
	for _, archived := range source.Rows {
		row := cloneRow(archived)
		for _, column := range out.Columns {
			if value, ok := reset[column.Name]; ok {
				row[column.Name] = value
			}
			if row[column.Name].Null && !columns[column.Name].Nullable {
				return Table{}, ErrSchema
			}
		}
		if source.Name == "instances" {
			set(row, "storage_class", Text(placement.StorageClass))
			if placement.PVC != "" {
				set(row, "pvc_name", Text(placement.PVC))
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}
