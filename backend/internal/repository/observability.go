package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"clawreef/internal/observability"
	"github.com/upper/db/v4"
)

type observationRuntime struct {
	failures sync.Map
	sess     db.Session
	journal  *observability.Journal
	objects  *observability.EncryptedObjects
}

var installedObservability atomic.Pointer[observationRuntime]

// ConfigureObservability must run before serving. The supplied session uses the
// ordinary backup-fenced connection; journal storage must be a durable shared mount.
func ConfigureObservability(sess db.Session, journal *observability.Journal, objects *observability.EncryptedObjects) func() {
	var next *observationRuntime
	if sess != nil && journal != nil && objects != nil {
		next = &observationRuntime{sess: sess, journal: journal, objects: objects}
	}
	prior := installedObservability.Swap(next)
	return func() { installedObservability.CompareAndSwap(next, prior) }
}
func ObservabilityEnabled() bool { return installedObservability.Load() != nil }
func observationOwner(id *int) string {
	if id == nil {
		return "system"
	}
	return "user:" + strconv.Itoa(*id)
}
func BeginObservedRequest(ctx context.Context, userID int, requestID string) error {
	r := installedObservability.Load()
	if r == nil {
		return nil
	}
	owner := observationOwner(&userID)
	// SQL tombstone plus journal admission prevent replaying external side effects
	// after a completed journal has been reclaimed.
	var prior struct {
		State string `db:"state"`
	}
	err := r.sess.WithContext(ctx).Collection("observability_admissions").Find(db.Cond{"owner": owner, "request_id": requestID}).One(&prior)
	if err == nil {
		return observability.ErrRecoveryRequired
	}
	if err != db.ErrNoMoreRows {
		return err
	}
	if err = r.journal.Admit(ctx, owner, requestID); err != nil {
		return err
	}
	_, err = r.sess.SQL().ExecContext(ctx, "INSERT INTO observability_admissions(owner,request_id) VALUES(?,?)", owner, requestID)
	return err
}
func FinishObservedRequest(ctx context.Context, userID int, requestID string) error {
	r := installedObservability.Load()
	if r == nil {
		return nil
	}
	if e := CheckObservedRequest(ctx, userID, requestID); e != nil {
		return e
	}
	return r.journal.Finish(ctx, observationOwner(&userID), requestID)
}
func ReplayObservability(ctx context.Context, limit int) (int, error) {
	r := installedObservability.Load()
	if r == nil {
		return 0, nil
	}
	return r.journal.ReplayWithTerminal(ctx, limit, r.project, func(ctx context.Context, state observability.RecoveryState) error {
		_, e := r.sess.SQL().ExecContext(ctx, `UPDATE observability_admissions SET state='terminal',completed_at=UTC_TIMESTAMP(6) WHERE owner=? AND request_id=?`, state.Owner, state.RequestID)
		return e
	})
}
func ObservabilityRecovery(ctx context.Context) ([]observability.RecoveryState, error) {
	r := installedObservability.Load()
	if r == nil {
		return nil, nil
	}
	return r.journal.Recovery(ctx)
}
func observationFields(value interface{}) (owner, request, uid string) {
	v := reflect.ValueOf(value).Elem()
	owner = "system"
	if f := v.FieldByName("UserID"); f.IsValid() && !f.IsNil() {
		owner = "user:" + strconv.FormatInt(f.Elem().Int(), 10)
	}
	f := v.FieldByName("RequestID")
	if f.Kind() == reflect.Ptr {
		if !f.IsNil() {
			request = f.Elem().String()
		}
	} else {
		request = f.String()
	}
	f = v.FieldByName("EventUID")
	if !f.IsNil() {
		uid = f.Elem().String()
	}
	return
}
func observationUID(table string, value interface{}) string {
	v := reflect.ValueOf(value).Elem()
	clone := reflect.New(v.Type())
	clone.Elem().Set(v)
	for _, name := range []string{"ID", "CreatedAt", "RecordedAt", "CompletedAt", "EventUID"} {
		f := clone.Elem().FieldByName(name)
		if f.IsValid() {
			f.Set(reflect.Zero(f.Type()))
		}
	}
	owner, request, _ := observationFields(value)
	var identity interface{} = clone.Interface()
	switch item := value.(type) {
	case *models.ModelInvocation:
		identity = []interface{}{owner, request, item.AttemptNo, item.ProviderType, item.ActualProviderModel}
	case *models.ChatMessageRecord:
		identity = []interface{}{owner, item.TraceID, item.SequenceNo, item.Role, item.Content}
	}
	raw, _ := json.Marshal(identity)
	hash := sha256.Sum256(append([]byte(table+"\x00"), raw...))
	return hex.EncodeToString(hash[:])
}

// persistObservation writes the encrypted journal before attempting SQL. Returning
// nil after a SQL outage means durable pending delivery, never that SQL committed.
func persistObservation(ctx context.Context, sess db.Session, table string, value interface{}) (bool, error) {
	if installedObservability.Load() != nil {
		release, e := backupgate.HoldWorkUnit(ctx, "observability.persist")
		if e != nil {
			return true, e
		}
		defer release()
	}
	r := installedObservability.Load()
	if r == nil {
		return false, nil
	}
	v := reflect.ValueOf(value).Elem()
	owner, request, uid := observationFields(value)
	if uid == "" {
		uid = observationUID(table, value)
		v.FieldByName("EventUID").Set(reflect.ValueOf(&uid))
	}
	if request == "" {
		request = "event:" + uid
	}
	clone := reflect.New(v.Type())
	clone.Elem().Set(v)
	clone.Elem().FieldByName("ID").SetInt(0)
	raw, e := json.Marshal(clone.Interface())
	if e != nil {
		return true, e
	}
	event := observability.Event{UID: uid, Owner: owner, RequestID: request, Kind: table, Payload: raw}
	e = r.journal.Append(ctx, event)
	standalone := false
	if errors.Is(e, os.ErrNotExist) {
		request = "event:" + uid
		event.RequestID = request
		e = r.journal.Append(ctx, event)
		if errors.Is(e, os.ErrNotExist) {
			if e = r.journal.Admit(ctx, owner, request); e != nil {
				return true, e
			}
			standalone = true
			e = r.journal.Append(ctx, event)
		}
	}
	if e != nil {
		r.failures.Store(owner+"\x00"+request, e)
		return true, e
	}
	if standalone {
		if e = r.journal.Finish(ctx, owner, request); e != nil {
			return true, e
		}
	}
	projectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	projected, e := r.projectValue(projectCtx, sess, event)
	cancel()
	if e == nil {
		reflect.ValueOf(value).Elem().FieldByName("ID").SetInt(reflect.ValueOf(projected).Elem().FieldByName("ID").Int())
		if e = r.journal.Delivered(ctx, event); e != nil {
			return true, e
		}
	}
	// The durable event is preserved for replay when SQL or objects are down.
	return true, nil
}
func decodeObservation(event observability.Event) (interface{}, error) {
	var value interface{}
	switch event.Kind {
	case "model_invocations":
		value = &models.ModelInvocation{}
	case "audit_events":
		value = &models.AuditEvent{}
	case "cost_records":
		value = &models.CostRecord{}
	case "chat_messages":
		value = &models.ChatMessageRecord{}
	default:
		return nil, fmt.Errorf("unregistered observation kind %q", event.Kind)
	}
	e := json.Unmarshal(event.Payload, value)
	return value, e
}
func (r *observationRuntime) project(ctx context.Context, event observability.Event) error {
	_, e := r.projectValue(ctx, r.sess, event)
	return e
}
func (r *observationRuntime) projectValue(ctx context.Context, sess db.Session, event observability.Event) (interface{}, error) {
	value, e := decodeObservation(event)
	if e != nil {
		return nil, e
	}
	if id, found, err := observationReceipt(ctx, sess, event); err != nil {
		return nil, err
	} else if found {
		reflect.ValueOf(value).Elem().FieldByName("ID").SetInt(int64(id))
		return value, nil
	}
	refs, e := r.externalize(ctx, value)
	if e != nil {
		return nil, e
	}
	e = sess.WithContext(ctx).Tx(func(tx db.Session) error {
		// Take the trace lock before inserting any source row. Taking it only
		// during aggregation deadlocks against another writer's uncommitted row.
		trace := ""
		switch v := value.(type) {
		case *models.ModelInvocation:
			trace = v.TraceID
		case *models.AuditEvent:
			trace = v.TraceID
		case *models.CostRecord:
			trace = v.TraceID
		}
		if trace != "" {
			if err := lockAuditTraces(ctx, tx, []string{trace}); err != nil {
				return err
			}
		}
		if id, found, err := observationReceipt(ctx, tx, event); err != nil {
			return err
		} else if found {
			reflect.ValueOf(value).Elem().FieldByName("ID").SetInt(int64(id))
			return nil
		}
		// Locking the unique identity makes a retry after a lost COMMIT response safe.
		var existing struct {
			ID int `db:"id"`
		}
		err := tx.Collection(event.Kind).Find(db.Cond{"event_uid": event.UID}).One(&existing)
		if err == nil {
			reflect.ValueOf(value).Elem().FieldByName("ID").SetInt(int64(existing.ID))
			return nil
		}
		if err != db.ErrNoMoreRows {
			return err
		}
		for field, ref := range refs {
			serialized, _ := json.Marshal(ref)
			if _, err = tx.SQL().ExecContext(ctx, `INSERT INTO observability_objects(object_key,owner,sha256,byte_length,encryption_version,retain_until) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6)+INTERVAL 30 DAY) ON DUPLICATE KEY UPDATE retain_until=GREATEST(retain_until,VALUES(retain_until))`, ref.ObjectKey, ref.Owner, ref.SHA256, ref.ByteLength, ref.EncryptionVersion); err != nil {
				return err
			}
			if _, err = tx.SQL().ExecContext(ctx, `INSERT INTO observability_body_refs(source_table,event_uid,field_name,object_key,body_ref) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE object_key=VALUES(object_key),body_ref=VALUES(body_ref),released_at=NULL`, event.Kind, event.UID, field, ref.ObjectKey, string(serialized)); err != nil {
				return err
			}
		}
		// Deferred dependencies resolve by stable request identity; zero is not an FK.
		rv := reflect.ValueOf(value).Elem()
		if field := rv.FieldByName("InvocationID"); field.IsValid() && (field.IsNil() || field.Elem().Int() == 0) {
			var invocation struct {
				ID int `db:"id"`
			}
			cond := db.Cond{"request_id": reflectRequestID(value), "user_id": rv.FieldByName("UserID").Interface()}
			exact := rv.FieldByName("InvocationEventUID")
			if exact.IsValid() && !exact.IsNil() {
				cond = db.Cond{"event_uid": exact.Elem().String()}
			}
			err = tx.Collection("model_invocations").Find(cond).OrderBy("-attempt_no", "-id").One(&invocation)
			if err == db.ErrNoMoreRows && exact.IsValid() && !exact.IsNil() {
				return errors.New("invocation dependency awaiting durable replay")
			}
			if err == nil {
				field.Set(reflect.ValueOf(&invocation.ID))
			} else if err != db.ErrNoMoreRows {
				return err
			} else {
				field.Set(reflect.Zero(field.Type()))
			}
		}
		if err := lockObservationSourceDays(ctx, tx, value); err != nil {
			return err
		}
		res, err := tx.Collection(event.Kind).Insert(value)
		if err != nil {
			return err
		}
		rv.FieldByName("ID").SetInt(res.ID().(int64))
		if _, err = tx.SQL().ExecContext(ctx, `INSERT INTO observability_event_receipts(event_uid,source_table,original_id,payload_sha256) VALUES(?,?,?,?)`, event.UID, event.Kind, res.ID().(int64), observability.EventDigest(event.Payload)); err != nil {
			return err
		}
		return maintainObservationSummary(ctx, tx, event.Kind, value)
	})
	if e != nil {
		if id, found, err := observationReceipt(ctx, sess, event); err == nil && found {
			reflect.ValueOf(value).Elem().FieldByName("ID").SetInt(int64(id))
			return value, nil
		}
	}

	return value, e
}
func bodyFields(value interface{}) map[string]string {
	switch value.(type) {
	case *models.ModelInvocation:
		return map[string]string{"RequestPayload": "RequestBodyRef", "ResponsePayload": "ResponseBodyRef"}
	case *models.AuditEvent:
		return map[string]string{"Details": "DetailsBodyRef"}
	case *models.ChatMessageRecord:
		return map[string]string{"Content": "ContentBodyRef"}
	}
	return nil
}
func (r *observationRuntime) externalize(ctx context.Context, value interface{}) (map[string]observability.BodyRef, error) {
	refs := map[string]observability.BodyRef{}
	v := reflect.ValueOf(value).Elem()
	owner, _, _ := observationFields(value)
	for field, refField := range bodyFields(value) {
		f := v.FieldByName(field)
		var body string
		if f.Kind() == reflect.Ptr {
			if f.IsNil() {
				continue
			}
			body = f.Elem().String()
		} else {
			body = f.String()
		}
		if len(body) < 4096 {
			continue
		}
		intent := r.objects.Reference(owner, []byte(body))
		if _, err := r.sess.SQL().ExecContext(ctx, `INSERT INTO observability_objects(object_key,owner,sha256,byte_length,encryption_version,retain_until) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6)+INTERVAL 30 DAY) ON DUPLICATE KEY UPDATE retain_until=GREATEST(retain_until,VALUES(retain_until)),deleted_at=NULL`, intent.ObjectKey, intent.Owner, intent.SHA256, intent.ByteLength, intent.EncryptionVersion); err != nil {
			return nil, err
		}
		ref, e := r.objects.Put(ctx, owner, []byte(body))
		if e != nil {
			return nil, e
		}
		raw, _ := json.Marshal(ref)
		encoded := string(raw)
		v.FieldByName(refField).Set(reflect.ValueOf(&encoded))
		f.Set(reflect.Zero(f.Type()))
		refs[field] = ref
	}
	return refs, nil
}
func hydrateObservation(ctx context.Context, value interface{}) error {
	if len(bodyFields(value)) == 0 {
		return nil
	}
	r := installedObservability.Load()
	if r == nil {
		return nil
	}
	v := reflect.ValueOf(value).Elem()
	owner, _, uid := observationFields(value)
	for field, refField := range bodyFields(value) {
		rf := v.FieldByName(refField)
		if rf.IsNil() {
			continue
		}
		var ref observability.BodyRef
		if e := json.Unmarshal([]byte(rf.Elem().String()), &ref); e != nil {
			return e
		}
		if ref.Owner != owner {
			return errors.New("body owner mismatch")
		}
		if uid != "" {
			table := value.(interface{ TableName() string }).TableName()
			row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM observability_body_refs b JOIN observability_objects o ON o.object_key=b.object_key WHERE b.source_table=? AND b.event_uid=? AND b.field_name=? AND b.released_at IS NOT NULL AND BINARY b.object_key=? AND b.body_ref=CAST(? AS JSON) AND BINARY o.owner=? AND BINARY o.sha256=? AND o.byte_length=? AND o.encryption_version=?)`, table, uid, field, ref.ObjectKey, rf.Elem().String(), owner, ref.SHA256, ref.ByteLength, ref.EncryptionVersion)
			if err != nil {
				return err
			}
			var expired bool
			if err = row.Scan(&expired); err != nil {
				return err
			}
			if expired {
				setObservationBodyStatus(v, field, "expired")
				continue
			}
		}
		body, e := r.objects.Get(ctx, owner, ref)
		if e != nil {
			return fmt.Errorf("body unavailable (metadata retained): %w", e)
		}
		setObservationBodyStatus(v, field, "available")
		text := string(body)
		f := v.FieldByName(field)
		if f.Kind() == reflect.Ptr {
			f.Set(reflect.ValueOf(&text))
		} else {
			f.SetString(text)
		}
	}
	return nil
}
func hydrateObservations(ctx context.Context, items interface{}) error {
	v := reflect.ValueOf(items)
	for i := 0; i < v.Len(); i++ {
		if e := hydrateObservation(ctx, v.Index(i).Addr().Interface()); e != nil {
			return e
		}
	}
	return nil
}

// RunObservability retries at bounded intervals. It never invokes a model or any
// budget finalizer: event projection cannot repeat externally visible execution.
func RunObservability(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			work, cancel := context.WithTimeout(ctx, 25*time.Second)
			_, e := ReplayObservability(work, 500)
			cancel()
			if e != nil && onError != nil {
				onError(e)
			}
		}
	}
}

func reflectRequestID(value interface{}) string {
	_, request, _ := observationFields(value)
	return request
}

func ObservationAttemptUID(table string, userID int, request string, attempt int, provider, model string) string {
	raw, _ := json.Marshal([]interface{}{observationOwner(&userID), request, attempt, provider, model})
	hash := sha256.Sum256(append([]byte(table+"\x00"), raw...))
	return hex.EncodeToString(hash[:])
}

func CheckObservedRequest(ctx context.Context, userID int, request string) error {
	r := installedObservability.Load()
	if r == nil {
		return nil
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if failure, ok := r.failures.Load(observationOwner(&userID) + "\x00" + request); ok {
		return fmt.Errorf("durable observation incomplete; execution outcome requires recovery: %w", failure.(error))
	}
	return nil
}

// DurableArchivePut shares the configured owner-scoped encrypted object boundary.
// Archive bytes are retained for at least 365 days and tracked before upload.
func DurableArchivePut(ctx context.Context, owner string, raw []byte) (json.RawMessage, error) {
	return durableArchivePut(ctx, owner, "", raw)
}
func durableArchivePut(ctx context.Context, owner, kind string, raw []byte) (json.RawMessage, error) {
	r := installedObservability.Load()
	if r == nil {
		return nil, errors.New("durable observability not configured")
	}
	ref := r.objects.Reference(owner, raw)
	if _, e := r.sess.SQL().ExecContext(ctx, `INSERT INTO observability_objects(object_key,owner,sha256,byte_length,encryption_version,retain_until) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6)+INTERVAL 365 DAY) ON DUPLICATE KEY UPDATE retain_until=GREATEST(retain_until,VALUES(retain_until)),deleted_at=NULL`, ref.ObjectKey, ref.Owner, ref.SHA256, ref.ByteLength, ref.EncryptionVersion); e != nil {
		return nil, e
	}
	ref, e := r.objects.Put(ctx, owner, raw)
	if e != nil {
		return nil, e
	}
	if err := indexObservationArchive(ctx, r, ref, kind, raw); err != nil {
		return nil, err
	}
	encoded, e := json.Marshal(ref)
	return encoded, e
}
func DurableArchiveRead(ctx context.Context, owner string, encoded json.RawMessage) ([]byte, error) {
	r := installedObservability.Load()
	if r == nil {
		return nil, errors.New("durable observability not configured")
	}
	var ref observability.BodyRef
	if e := json.Unmarshal(encoded, &ref); e != nil {
		return nil, e
	}
	return r.objects.Get(ctx, owner, ref)
}

func setObservationBodyStatus(v reflect.Value, field, status string) {
	f := v.FieldByName("BodyStatus")
	if !f.IsValid() {
		return
	}
	if f.IsNil() {
		f.Set(reflect.MakeMap(f.Type()))
	}
	f.SetMapIndex(reflect.ValueOf(field), reflect.ValueOf(status))
}

func PreflightObservedRequest(ctx context.Context, userID int, request string) error {
	r := installedObservability.Load()
	if r == nil {
		return nil
	}
	if e := CheckObservedRequest(ctx, userID, request); e != nil {
		return e
	}
	return r.journal.CheckCompletionReserve(ctx, observationOwner(&userID), request, 16<<20, 8)
}

// HydrateArchivedPayload is called only after the archive reader has authorized
// the owner. The embedded owner is checked again by encrypted object retrieval.
func HydrateArchivedPayload(ctx context.Context, kind string, raw json.RawMessage) (json.RawMessage, error) {
	value := observationBodyModel(kind)
	if value == nil {
		return raw, nil
	}
	if e := json.Unmarshal(raw, value); e != nil {
		return nil, e
	}
	if e := hydrateObservation(ctx, value); e != nil {
		return nil, e
	}
	return json.Marshal(value)
}

func observationReceipt(ctx context.Context, sess db.Session, event observability.Event) (int, bool, error) {
	var receipt struct {
		ID   int    `db:"original_id"`
		Kind string `db:"source_table"`
		Hash string `db:"payload_sha256"`
	}
	e := sess.WithContext(ctx).Collection("observability_event_receipts").Find(db.Cond{"event_uid": event.UID}).One(&receipt)
	if e == db.ErrNoMoreRows {
		return 0, false, nil
	}
	if e != nil {
		return 0, false, e
	}
	if receipt.Kind != event.Kind || receipt.Hash != observability.EventDigest(event.Payload) {
		return 0, false, observability.ErrIdentityConflict
	}
	return receipt.ID, true, nil
}
