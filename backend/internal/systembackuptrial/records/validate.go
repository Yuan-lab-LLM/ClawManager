package records

import (
	"strconv"
	"strings"
)

func validateSnapshot(s Snapshot) error {
	if s.Version != Version {
		return ErrSchema
	}
	seen := map[string]bool{}
	for _, t := range s.Tables {
		if trialColumns[t.Name] == "" || seen[t.Name] || t.Engine != "InnoDB" || len(t.Columns) == 0 || len(t.PrimaryKey) == 0 {
			return ErrSchema
		}
		seen[t.Name] = true
		cols := map[string]Column{}
		for _, c := range t.Columns {
			if !identifier.MatchString(c.Name) {
				return ErrSchema
			}
			if _, ok := cols[c.Name]; ok {
				return ErrSchema
			}
			cols[c.Name] = c
		}
		keys := map[string]bool{}
		for _, r := range t.Rows {
			if len(r) != len(cols) {
				return ErrData
			}
			for name, c := range cols {
				v, ok := r[name]
				if !ok || (v.Null && (!c.Nullable || len(v.Bytes) > 0)) {
					return ErrData
				}
			}
			var key string
			for _, k := range t.PrimaryKey {
				v, ok := r[k]
				if !ok || v.Null {
					return ErrData
				}
				key += strconv.Itoa(len(v.Bytes)) + ":" + v.String()
			}
			if keys[key] {
				return ErrData
			}
			keys[key] = true
		}
	}
	if !seen["users"] || !seen["instances"] || !seen["user_quotas"] {
		return ErrSchema
	}
	return nil
}

func validateTrialRelationships(s Snapshot) error {
	users := map[string]bool{}
	instances := map[string]bool{}
	admin := false
	for _, t := range s.Tables {
		if t.Name != "users" {
			continue
		}
		for _, r := range t.Rows {
			id := r["id"].String()
			if !positiveID(id) || users[id] {
				return ErrData
			}
			users[id] = true
			provider := r["auth_provider"].String()
			if provider != "" && provider != "local" && provider != "ldap" {
				return ErrData
			}
			if (provider == "" || provider == "local") && len(r["password_hash"].Bytes) == 0 {
				return ErrData
			}
			if r["role"].String() == "admin" && r["is_active"].String() == "1" {
				admin = true
			}
		}
	}
	if !admin {
		return ErrData
	}
	for _, t := range s.Tables {
		if t.Name != "instances" {
			continue
		}
		for _, r := range t.Rows {
			id := r["id"].String()
			if !positiveID(id) || instances[id] || !users[r["user_id"].String()] {
				return ErrData
			}
			instances[id] = true
		}
	}
	for _, t := range s.Tables {
		for _, r := range t.Rows {
			if v, ok := r["user_id"]; ok && !v.Null && !users[v.String()] {
				return ErrData
			}
			if v, ok := r["instance_id"]; ok && !v.Null && !instances[v.String()] {
				return ErrData
			}
		}
	}
	return nil
}

func positiveID(s string) bool {
	v, e := strconv.ParseInt(s, 10, 64)
	return e == nil && v > 0 && strconv.FormatInt(v, 10) == s
}

func generatedColumn(extra string) bool {
	// MySQL marks ordinary TIMESTAMP defaults as DEFAULT_GENERATED. Those
	// columns accept explicit archived values. Only generated columns (STORED
	// GENERATED / VIRTUAL GENERATED) prohibit these inserts.
	for _, field := range strings.Fields(strings.ToUpper(extra)) {
		if field == "GENERATED" {
			return true
		}
	}
	return false
}

func hasColumn(t Table, name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func set(r Row, name string, value Cell) {
	if _, ok := r[name]; ok {
		r[name] = value
	}
}
