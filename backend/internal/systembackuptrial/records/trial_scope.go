package records

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
)

const maxTrialSelection = 256
const trialOptionPageSize = 100

var ErrTrialScope = errors.New("trial backup scope is invalid or changed")

type TrialInstanceChoice struct {
	ID     int64 `json:"id"`
	UserID int64 `json:"user_id"`
}

type TrialScope struct {
	UserIDs   []int64               `json:"user_ids"`
	Instances []TrialInstanceChoice `json:"instances"`
}

func (s TrialScope) Valid() bool {
	if len(s.UserIDs)+len(s.Instances) == 0 || len(s.UserIDs) > maxTrialSelection || len(s.Instances) > maxTrialSelection {
		return false
	}
	users := make(map[int64]bool)
	for _, id := range s.UserIDs {
		if id <= 0 || users[id] {
			return false
		}
		users[id] = true
	}
	instances := make(map[int64]bool)
	for _, item := range s.Instances {
		if item.ID <= 0 || item.UserID <= 0 || instances[item.ID] {
			return false
		}
		instances[item.ID] = true
	}
	return true
}

func (s TrialScope) instancePredicate(alias string) capturePredicate {
	if len(s.Instances) == 0 {
		return capturePredicate{clause: "0 = 1"}
	}
	args := make([]any, 0, 2*len(s.Instances))
	for _, choice := range s.Instances {
		args = append(args, choice.ID, choice.UserID)
	}
	return capturePredicate{
		clause: "(" + alias + "id, " + alias + "user_id) IN (" + strings.Repeat("(?,?),", len(s.Instances)-1) + "(?,?))",
		args:   args,
	}
}

func (s TrialScope) predicates() map[string]capturePredicate {
	instance := s.instancePredicate("")
	joined := s.instancePredicate("i.")
	owners := func(column string) capturePredicate {
		clause := column + " IN (SELECT i.user_id FROM instances i WHERE " + joined.clause + ")"
		if len(s.UserIDs) == 0 {
			return capturePredicate{clause: " WHERE " + clause, args: joined.args}
		}
		args := make([]any, 0, len(s.UserIDs))
		for _, id := range s.UserIDs {
			args = append(args, id)
		}
		return capturePredicate{
			clause: " WHERE (" + column + " IN (" + strings.Repeat("?,", len(args)-1) + "?) OR " + clause + ")",
			args:   slices.Concat(args, joined.args),
		}
	}
	return map[string]capturePredicate{
		"users":       owners("id"),
		"user_quotas": owners("user_id"),
		"instances":   {clause: " WHERE " + instance.clause, args: instance.args},
		"openclaw_injection_snapshots": {
			clause: " WHERE EXISTS (SELECT 1 FROM instances i WHERE i.openclaw_config_snapshot_id = openclaw_injection_snapshots.id AND " + joined.clause + ")",
			args:   joined.args,
		},
		"instance_desired_state": {
			clause: " WHERE desired_config_revision_id IS NOT NULL AND instance_id IN (SELECT i.id FROM instances i WHERE " + joined.clause + ")",
			args:   joined.args,
		},
		"instance_config_revisions": {
			clause: " WHERE EXISTS (SELECT 1 FROM instance_desired_state d JOIN instances i ON i.id = d.instance_id WHERE d.desired_config_revision_id = instance_config_revisions.id AND d.instance_id = instance_config_revisions.instance_id AND " + joined.clause + ")",
			args:   joined.args,
		},
	}
}

// CaptureSelectedTrial reads only selected accounts, instance owners and their
// required startup rows inside the same repeatable-read source transaction.
func CaptureSelectedTrial(ctx context.Context, db *sql.DB, limits Limits, database, uuid string, selection TrialScope) (Snapshot, error) {
	if !selection.Valid() {
		return Snapshot{}, ErrTrialScope
	}
	snapshot, err := captureTrial(ctx, db, limits, database, uuid, selection.predicates())
	if err != nil {
		return Snapshot{}, err
	}
	if err := matchTrialScope(snapshot, selection); err != nil {
		return Snapshot{}, err
	}
	return snapshot, ValidateTrial(snapshot)
}

func matchTrialScope(snapshot Snapshot, selection TrialScope) error {
	owners := make(map[string]bool)
	requested := make(map[string]string)
	for _, id := range selection.UserIDs {
		owners[strconv.FormatInt(id, 10)] = true
	}
	for _, choice := range selection.Instances {
		owner := strconv.FormatInt(choice.UserID, 10)
		owners[owner] = true
		requested[strconv.FormatInt(choice.ID, 10)] = owner
	}
	admin := false
	for _, table := range snapshot.Tables {
		switch table.Name {
		case "users":
			if len(table.Rows) != len(owners) {
				return ErrTrialScope
			}
			for _, row := range table.Rows {
				if !owners[row["id"].String()] {
					return ErrTrialScope
				}
				admin = admin || (row["role"].String() == "admin" && row["is_active"].String() == "1")
			}
		case "instances":
			if len(table.Rows) != len(requested) {
				return ErrTrialScope
			}
			for _, row := range table.Rows {
				owner, ok := requested[row["id"].String()]
				if !ok || owner != row["user_id"].String() {
					return ErrTrialScope
				}
			}
		case "instance_desired_state", "instance_config_revisions":
			column := "updated_by"
			if table.Name == "instance_config_revisions" {
				column = "published_by"
			}
			for _, row := range table.Rows {
				if !row[column].Null && !owners[row[column].String()] {
					row[column] = Cell{Null: true}
				}
			}
		}
	}
	if !admin {
		return ErrTrialScope
	}
	return nil
}

type TrialUserOption struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Admin bool   `json:"active_admin"`
}

type TrialInstanceOption struct {
	TrialInstanceChoice
	Name       string `json:"name"`
	Type       string `json:"type"`
	Mode       string `json:"mode"`
	OwnerAdmin bool   `json:"owner_active_admin"`
}

type TrialOptions struct {
	Users          []TrialUserOption     `json:"users"`
	Instances      []TrialInstanceOption `json:"instances"`
	NextUserID     int64                 `json:"next_user_id,omitempty"`
	NextInstanceID int64                 `json:"next_instance_id,omitempty"`
}

// CheckTrialLiteOwner verifies the registered owner and runtime against the source.
func CheckTrialLiteOwner(ctx context.Context, db *sql.DB, database, uuid string, choice TrialInstanceChoice, runtime string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return ErrDatabase
	}
	defer conn.Close()
	if err := trialSourceSession(ctx, conn, database, uuid); err != nil {
		return err
	}
	var matches bool
	err = conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM instances WHERE id = ? AND user_id = ?
		AND type = ? AND instance_mode = 'lite' AND runtime_type = 'gateway')`, choice.ID, choice.UserID, runtime).Scan(&matches)
	if err != nil {
		return ErrDatabase
	}
	if !matches {
		return ErrTrialScope
	}
	return nil
}

func ListTrialOptions(ctx context.Context, db *sql.DB, database, uuid string, afterUser, afterInstance int64) (TrialOptions, error) {
	var options TrialOptions
	conn, err := db.Conn(ctx)
	if err != nil {
		return options, ErrDatabase
	}
	defer conn.Close()
	if err := trialSourceSession(ctx, conn, database, uuid); err != nil {
		return options, err
	}
	return readTrialOptions(ctx, conn, afterUser, afterInstance)
}

func readTrialOptions(ctx context.Context, conn *sql.Conn, afterUser, afterInstance int64) (TrialOptions, error) {
	out := TrialOptions{Users: []TrialUserOption{}, Instances: []TrialInstanceOption{}}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, ErrDatabase
	}
	defer tx.Rollback()
	users, err := tx.QueryContext(ctx, "SELECT id, username, COALESCE(role = 'admin' AND is_active = 1, 0) FROM users WHERE id > ? ORDER BY id LIMIT ?", afterUser, trialOptionPageSize+1)
	if err != nil {
		return out, ErrDatabase
	}
	defer users.Close()
	for users.Next() {
		var option TrialUserOption
		if err := users.Scan(&option.ID, &option.Name, &option.Admin); err != nil {
			return out, ErrDatabase
		}
		if len(out.Users) == trialOptionPageSize {
			out.NextUserID = out.Users[len(out.Users)-1].ID
			break
		}
		out.Users = append(out.Users, option)
	}
	if err := users.Close(); err != nil || users.Err() != nil {
		return out, ErrDatabase
	}
	instances, err := tx.QueryContext(ctx, `SELECT i.id, i.user_id, i.name, COALESCE(i.type, ''), COALESCE(i.instance_mode, ''),
		COALESCE(u.role = 'admin' AND u.is_active = 1, 0) FROM instances i LEFT JOIN users u ON u.id = i.user_id
		WHERE i.id > ? ORDER BY i.id LIMIT ?`, afterInstance, trialOptionPageSize+1)
	if err != nil {
		return out, ErrDatabase
	}
	defer instances.Close()
	for instances.Next() {
		var option TrialInstanceOption
		if err := instances.Scan(&option.ID, &option.UserID, &option.Name, &option.Type, &option.Mode, &option.OwnerAdmin); err != nil {
			return out, ErrDatabase
		}
		if len(out.Instances) == trialOptionPageSize {
			out.NextInstanceID = out.Instances[len(out.Instances)-1].ID
			break
		}
		out.Instances = append(out.Instances, option)
	}
	if err := instances.Close(); err != nil || instances.Err() != nil {
		return out, ErrDatabase
	}
	if tx.Commit() != nil {
		return out, ErrDatabase
	}
	return out, nil
}
