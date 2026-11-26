package records

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
)

// The trial keeps deployment-owned defaults already present on a fresh
// migrated target. These rows are never restored from the source.
func trialSeedLimit(name string) int {
	switch name {
	case "system_image_settings", "skill_hub_tags":
		return 128
	case "instance_lifecycle_scheduler", "northbound_admin_settings":
		return 1
	default:
		return 0
	}
}

func validTrialSeedRow(table string, row Row) bool {
	switch table {
	case "instance_lifecycle_scheduler":
		return row["id"].String() == "1" && row["revision"].String() == "0"
	case "northbound_admin_settings":
		return row["id"].String() == "1" && row["version"].String() == "1" &&
			row["updated_by"].Null && row["external_node_port"].String() == "0"
	default:
		return true
	}
}

func captureTrialSeeds(ctx context.Context, q queryer, catalog []Table) (map[string][32]byte, error) {
	result := make(map[string][32]byte)
	for _, table := range catalog {
		if trialSeedLimit(table.Name) == 0 {
			continue
		}
		digest, err := trialSeedDigest(ctx, q, table)
		if err != nil {
			return nil, err
		}
		result[table.Name] = digest
	}
	return result, nil
}

// Fingerprint the reviewed target defaults in the restore transaction and
// compare them independently by primary key after commit. The rows themselves
// never appear in the backup, task metadata or restoration plan.
func trialSeedDigest(ctx context.Context, q queryer, table Table) ([32]byte, error) {
	var digest [32]byte
	rowLimit := trialSeedLimit(table.Name)
	if rowLimit == 0 || table.Engine != "InnoDB" || len(table.PrimaryKey) != 1 || table.PrimaryKey[0] != "id" || len(table.Columns) == 0 {
		return digest, ErrSchema
	}
	name, err := quoted(table.Name)
	if err != nil {
		return digest, err
	}
	var cols []string
	for _, column := range table.Columns {
		col, err := quoted(column.Name)
		if err != nil {
			return digest, err
		}
		cols = append(cols, col)
	}
	rows, err := q.QueryContext(ctx, "SELECT "+strings.Join(cols, ",")+" FROM "+name+" ORDER BY `id`")
	if err != nil {
		return digest, ErrDatabase
	}
	defer rows.Close()
	h := sha256.New()
	h.Write([]byte(SchemaDigest(table)))
	encoder := json.NewEncoder(h)
	scan := newCaptureScanSlots(len(cols))
	var count, size int
	for rows.Next() {
		count++
		if count > rowLimit {
			return digest, ErrNotEmpty
		}
		values, err := scan.scan(ctx, rows)
		if err != nil {
			return digest, ErrDatabase
		}
		row := make(Row, len(cols))
		for i, value := range values {
			cell, err := rawCell(value)
			if err != nil {
				return digest, err
			}
			size += len(cell.Bytes)
			if size > 256<<10 {
				return digest, ErrCapacity
			}
			row[table.Columns[i].Name] = cell
		}
		if !validTrialSeedRow(table.Name, row) {
			return digest, ErrNotEmpty
		}
		if err := encoder.Encode(row); err != nil {
			return digest, ErrData
		}
		clear(values)
	}
	if rows.Err() != nil {
		return digest, ErrDatabase
	}
	copy(digest[:], h.Sum(nil))
	return digest, nil
}
