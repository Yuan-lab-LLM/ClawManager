package systembackup

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"clawreef/internal/historymaintenance"
	"clawreef/internal/observability"
	"clawreef/internal/systembackup/team"
)

type historyInventory struct {
	live, deleted map[string]observability.BodyRef
	releases      map[observability.BodyField]observability.BodyRef
	sources       []observability.BodyDependency
	archiveKinds  map[string]string
	bodyKeys      map[string]bool
	edges         map[string][]string
}

func newHistoryInventory() *historyInventory {
	return &historyInventory{live: map[string]observability.BodyRef{}, deleted: map[string]observability.BodyRef{}, releases: map[observability.BodyField]observability.BodyRef{}, archiveKinds: map[string]string{}, bodyKeys: map[string]bool{}}
}

func (h *historyInventory) expired(dep observability.BodyDependency, kind string) bool {
	return kind != "" && dep.Field.Table == kind && dep.Field.UID != "" && h.releases[dep.Field] == dep.Ref && h.deleted[dep.Ref.ObjectKey] == dep.Ref
}

func (h *historyInventory) record(table SQLTable, row team.Row, limit int) error {
	err := h.recordRow(table.Name, row)
	if err == nil && len(h.sources)+len(h.live)+len(h.deleted)+len(h.releases) > limit {
		return errors.New("history dependency inventory exceeds bound")
	}
	return err
}

func (h *historyInventory) recordRow(table string, row team.Row) error {
	cell := func(name string) string { return string(row[name].Data) }
	present := func(name string) bool { v, ok := row[name]; return ok && !v.IsNull }
	monthlyKind, _, monthly := historymaintenance.ParseMonthly(table)
	inline := monthly && present("payload_json")
	if inline {
		var identity struct {
			ID    int64 `json:"id"`
			Owner int64 `json:"user_id"`
		}
		if err := json.Unmarshal(row["payload_json"].Data, &identity); err != nil || strconv.FormatInt(identity.ID, 10) != cell("original_id") || strconv.FormatInt(identity.Owner, 10) != cell("owner_id") {
			return errors.New("monthly payload identity differs from signed SQL")
		}
		deps, err := observability.ArchiveDependencies(row["payload_json"].Data)
		if err != nil {
			return err
		}
		owner := "system"
		if identity.Owner > 0 {
			owner = "user:" + strconv.FormatInt(identity.Owner, 10)
		}
		for _, dep := range deps {
			if dep.Ref.Owner != owner || (dep.Field.Table != "" && dep.Field.Table != monthlyKind) {
				return errors.New("monthly body owner differs from signed SQL")
			}
			h.bodyKeys[dep.Ref.ObjectKey] = true
			h.sources = append(h.sources, dep)
		}
	}
	if table == "observability_objects" {
		ref, err := snapshotObjectIdentity(row)
		if err != nil {
			return err
		}
		if !present("deleted_at") {
			h.live[ref.ObjectKey] = ref
		}
		if present("backup_verified_at") && present("restore_verified_at") && present("recovery_closed_at") {
			h.deleted[ref.ObjectKey] = ref
		}
		return nil
	}
	for _, column := range []string{"object_ref", "body_ref", "request_body_ref", "response_body_ref", "details_body_ref", "content_body_ref"} {
		if !present(column) {
			continue
		}
		if column == "object_ref" && inline && cell(column) == "null" {
			continue
		}
		var ref observability.BodyRef
		if err := json.Unmarshal(row[column].Data, &ref); err != nil || ref.Owner == "" || ref.ObjectKey == "" || !validHash(ref.SHA256) || ref.ByteLength < 0 || ref.ByteLength > maxRowBytes || ref.EncryptionVersion != 1 {
			return errors.New("invalid signed encrypted history reference")
		}
		field := observability.CanonicalBodyField(column, cell("event_uid"))
		if field.Table != "" {
			h.bodyKeys[ref.ObjectKey] = true
			owner := "system"
			if present("user_id") {
				owner = "user:" + cell("user_id")
			}
			if field.Table != table || owner != ref.Owner {
				return errors.New("canonical body owner differs from signed SQL")
			}
		}
		if table == "observability_body_refs" {
			h.bodyKeys[ref.ObjectKey] = true
			if cell("object_key") != ref.ObjectKey {
				return errors.New("field registration identity differs from signed reference")
			}
			if present("released_at") {
				h.releases[observability.BodyField{Table: cell("source_table"), UID: cell("event_uid"), Field: cell("field_name")}] = ref
				continue
			}
		}
		if column == "object_ref" {
			kind := cell("kind")
			if monthly, _, ok := historymaintenance.ParseMonthly(table); ok {
				kind = monthly
			}
			if present("owner_id") {
				id, err := strconv.ParseInt(cell("owner_id"), 10, 64)
				if err != nil || id < 1 || ref.Owner != "user:"+strconv.FormatInt(id, 10) {
					return errors.New("archive dependency owner differs from SQL owner")
				}
			}
			if old, ok := h.archiveKinds[ref.ObjectKey]; ok && old != kind {
				kind = ""
			}
			h.archiveKinds[ref.ObjectKey] = kind
		}
		h.sources = append(h.sources, observability.BodyDependency{Ref: ref, Field: field})
	}
	return nil
}

func snapshotObjectIdentity(row team.Row) (observability.BodyRef, error) {
	length, err := strconv.ParseInt(string(row["byte_length"].Data), 10, 64)
	ref := observability.BodyRef{ObjectKey: string(row["object_key"].Data), Owner: string(row["owner"].Data), SHA256: string(row["sha256"].Data), ByteLength: length, EncryptionVersion: 1}
	if err != nil || length < 0 || string(row["encryption_version"].Data) != "1" || ref.Owner == "" || !validHash(ref.SHA256) || !strings.HasPrefix(ref.ObjectKey, "observability/v1/") {
		return ref, errors.New("invalid signed observation object identity")
	}
	return ref, nil
}
