package records

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
)

var (
	ErrSchema        = errors.New("unsupported users/instances schema")
	ErrData          = errors.New("invalid users/instances data")
	ErrCapacity      = errors.New("users/instances capture capacity exceeded")
	ErrDatabase      = errors.New("users/instances database operation failed")
	ErrIsolation     = errors.New("restore target isolation not verified")
	ErrNotEmpty      = errors.New("restore target contains non-bootstrap data")
	ErrNormalization = errors.New("required normalization adapter unavailable or failed")
)

// Cell preserves NULL versus empty and raw MySQL representations (including
// decimal, unsigned BIGINT and binary), without float64/JSON DTO conversion.
type Cell struct {
	Null  bool   `json:"null,omitempty"`
	Bytes []byte `json:"bytes,omitempty"`
}

func Text(s string) Cell      { return Cell{Bytes: []byte(s)} }
func (c Cell) String() string { return string(c.Bytes) }

type Row map[string]Cell
type Column struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default,omitempty"`
	Extra    string  `json:"extra,omitempty"`
}
type ForeignKey struct {
	Column    string `json:"column"`
	Table     string `json:"table"`
	Reference string `json:"reference"`
}
type Table struct {
	Name        string       `json:"name"`
	Engine      string       `json:"engine"`
	Columns     []Column     `json:"columns"`
	PrimaryKey  []string     `json:"primary_key"`
	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`
	// Captured for the WorkBuddy identity contract; omitted for legacy payloads.
	UniqueKeys []UniqueKey `json:"unique_keys,omitempty"`
	Rows       []Row       `json:"rows"`
}
type UniqueKey struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}
type Snapshot struct {
	Version string  `json:"version"`
	Tables  []Table `json:"tables"`
}

const Version = "users-instances-rows.v1alpha1"

type Limits struct{ MaxRows, MaxBytes int64 }

func (l Limits) valid() bool { return l.MaxRows > 0 && l.MaxBytes > 0 }

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func quoted(s string) (string, error) {
	if !identifier.MatchString(s) {
		return "", ErrSchema
	}
	return "`" + s + "`", nil
}

// Digest must only be written to encrypted evidence: row hashes can disclose
// low-entropy data. It is not the global JCS manifest hash.
func Digest(s Snapshot) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", ErrData
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func cloneRow(r Row) Row {
	out := make(Row, len(r))
	for k, v := range r {
		out[k] = Cell{Null: v.Null, Bytes: append([]byte(nil), v.Bytes...)}
	}
	return out
}
func sortedTables(tables []Table) ([]Table, error) {
	// Keep foreign-key enforcement enabled. Unsupported cycles must be adapted,
	// never worked around with FOREIGN_KEY_CHECKS=0.
	pending := map[string]Table{}
	for _, t := range tables {
		if _, ok := pending[t.Name]; ok {
			return nil, ErrSchema
		}
		pending[t.Name] = t
	}
	var out []Table
	for len(pending) > 0 {
		var ready []string
		for name, t := range pending {
			blocked := false
			for _, fk := range t.ForeignKeys {
				if _, ok := pending[fk.Table]; ok {
					blocked = true
				}
			}
			if !blocked {
				ready = append(ready, name)
			}
		}
		if len(ready) == 0 {
			return nil, ErrSchema
		}
		sort.Strings(ready)
		for _, name := range ready {
			out = append(out, pending[name])
			delete(pending, name)
		}
	}
	return out, nil
}
