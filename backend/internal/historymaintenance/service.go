// Package historymaintenance owns bounded, fail-closed history retention.
// Monthly tables share the primary MySQL failure domain; encrypted archives
// and independently verified restore receipts are required before hot deletion.
package historymaintenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const BatchLimit = 500
const BatchDuration = 30 * time.Second
const SchemaVersion = 1

var Kinds = []string{"model_invocations", "audit_events", "chat_messages", "risk_hits", "team_events", "team_tasks"}

// Costs have monthly display copies, but their authoritative financial source
// is deliberately absent from the archive/retention Kinds above.
var MonthlyKinds = append(append([]string{}, Kinds...), "cost_records")

// Store must encrypt within owner scope, authenticate references, and preserve
// immutable identity. Put is retry-safe for an identical owner/key/content.
type Store interface {
	Put(context.Context, string, string, []byte) ([]byte, error)
	Read(context.Context, string, []byte) ([]byte, error)
}

// Gate participates in the existing system backup writer barrier. The returned
// release must remain held until all SQL and object mutations finish.
type Gate func(context.Context) (release func(), err error)

type Service struct {
	monthlyDDL *sql.DB
	DB         *sql.DB
	Store      Store
	Gate       Gate
	holder     string
}

func New(db *sql.DB, store Store, gate Gate) *Service {
	return &Service{DB: db, Store: store, Gate: gate, holder: fmt.Sprintf("history-%d", time.Now().UnixNano())}
}

type Record struct {
	Kind               string          `json:"kind"`
	ID                 int64           `json:"id"`
	OwnerID            int64           `json:"owner_id"`
	InstanceID         int64           `json:"instance_id"`
	TeamID             int64           `json:"team_id"`
	TraceID            string          `json:"trace_id"`
	SessionID          string          `json:"session_id"`
	CreatedAt          time.Time       `json:"created_at"`
	Month              string          `json:"month"`
	Payload            json.RawMessage `json:"payload,omitempty"`
	Reference          json.RawMessage `json:"-"`
	SHA256             string          `json:"sha256"`
	Bytes              int64           `json:"bytes"`
	ArchivedAt         time.Time       `json:"archived_at"`
	ReadbackVerifiedAt time.Time       `json:"-"`
	RestoreVerifiedAt  time.Time       `json:"-"`
	RestoreSHA256      string          `json:"-"`
	RestoreReceipt     string          `json:"-"`
	SourceDeletedAt    sql.NullTime    `json:"-"`
	Metadata           json.RawMessage `json:"metadata"`
	SummaryVerifiedAt  time.Time       `json:"-"`
	SummarySHA256      string          `json:"-"`
	Status             string          `json:"status"`
}

func (r Record) Removable() bool {
	return !r.ReadbackVerifiedAt.IsZero() && !r.RestoreVerifiedAt.IsZero() && r.SHA256 != "" && r.RestoreSHA256 == r.SHA256 && r.RestoreReceipt != "" && !r.SummaryVerifiedAt.IsZero() && r.SummarySHA256 == r.SHA256
}
func supported(kind string) bool {
	for _, v := range MonthlyKinds {
		if v == kind {
			return true
		}
	}
	return false
}
func MonthlyTable(kind string, at time.Time) (string, error) {
	if !supported(kind) {
		return "", errors.New("unsupported history kind")
	}
	return kind + "_" + at.UTC().Format("200601"), nil
}
func Owner(id int64) string { return "user:" + strconv.FormatInt(id, 10) }
func (s *Service) verifyArchive(ctx context.Context, r Record) (Record, error) {
	if s.Store == nil || r.OwnerID <= 0 || len(r.Payload) == 0 || !json.Valid(r.Payload) {
		return r, errors.New("archive owner/store/payload required")
	}
	h := sha256.Sum256(r.Payload)
	r.SHA256 = hex.EncodeToString(h[:])
	r.Bytes = int64(len(r.Payload))
	ref, err := s.Store.Put(ctx, Owner(r.OwnerID), fmt.Sprintf("history/%s/%d/%s", r.Kind, r.ID, r.SHA256), r.Payload)
	if err != nil {
		return r, err
	}
	if !json.Valid(ref) {
		return r, errors.New("archive reference must be valid JSON")
	}
	actual, err := s.Store.Read(ctx, Owner(r.OwnerID), ref)
	if err != nil {
		return r, err
	}
	if !bytes.Equal(actual, r.Payload) {
		return r, errors.New("archive readback checksum mismatch")
	}
	r.Reference = ref
	r.ArchivedAt = time.Now().UTC()
	r.ReadbackVerifiedAt = r.ArchivedAt
	r.Status = "archived_source_retained"
	r.Month, err = MonthlyTable(r.Kind, r.CreatedAt)
	return r, err
}
