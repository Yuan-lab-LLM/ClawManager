package observability

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"clawreef/internal/backupgate"
)

var ErrOverloaded = errors.New("observability durable capacity exhausted; request was not admitted")
var ErrIdentityConflict = errors.New("observability event identity conflicts with previously accepted content")
var ErrRecoveryRequired = errors.New("request was previously admitted; execution outcome requires recovery; do not repeat upstream")

const (
	AdmissionTerminal       = "terminal"
	AdmissionOutcomeUnknown = "terminal_unknown"
	abandonedAdmissionAge   = time.Hour

	journalCipherOverhead = 28 // AES-GCM nonce and tag in the existing WAL format
	journalLengthSize     = 4
	journalTerminalRoom   = 128
	journalFutureAckRoom  = 256
	journalGrowthBytes    = 1 << 20
)

type Limits struct {
	RequestBytes int64 // Initial physical reservation for each admission.
}
type Event struct {
	UID       string          `json:"event_uid"`
	Owner     string          `json:"owner"`
	RequestID string          `json:"request_id"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
}
type RecoveryState struct {
	Owner      string    `json:"owner"`
	RequestID  string    `json:"request_id"`
	State      string    `json:"state"`
	Pending    int       `json:"pending"`
	AdmittedAt time.Time `json:"admitted_at"`
}
type journalFrame struct {
	Type      string    `json:"type"`
	Version   int       `json:"version,omitempty"`
	Owner     string    `json:"owner,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	At        time.Time `json:"at,omitempty"`
	Event     *Event    `json:"event,omitempty"`
	UID       string    `json:"uid,omitempty"`
}
type journalState struct {
	RecoveryState
	version             int
	modified            time.Time
	events              map[string]Event
	delivered           map[string]bool
	pendingControlBytes int64
	offset              int64
	size                int64
	file                string
}
type Journal struct {
	dir    string
	crypto *EncryptedObjects
	limits Limits
}

func NewJournal(dir string, key []byte, limits Limits) (*Journal, error) {
	if !filepath.IsAbs(dir) || len(key) != 32 {
		return nil, errors.New("journal requires absolute persistent directory and stable 32-byte encryption key")
	}
	if limits.RequestBytes == 0 {
		limits.RequestBytes = 64 << 20
	}
	if limits.RequestBytes < 65536 {
		return nil, errors.New("invalid durable journal request reservation")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	info, e := os.Lstat(dir)
	if e != nil {
		return nil, e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("journal directory must be a real directory")
	}
	j := &Journal{dir: dir, crypto: &EncryptedObjects{master: append([]byte(nil), key...)}, limits: limits}
	// Reading all existing admissions authenticates the configured key before serving.
	e = j.locked(context.Background(), func() error { _, e := j.states(); return e })
	return j, e
}
func (j *Journal) key(owner, request string) string {
	h := hmac.New(sha256.New, j.crypto.master)
	h.Write([]byte("journal-v1\x00" + owner + "\x00" + request))
	return hex.EncodeToString(h.Sum(nil)) + ".wal"
}
func (j *Journal) locked(ctx context.Context, fn func() error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(j.dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
func (j *Journal) read(name string) (*journalState, error) {
	return j.readMode(name, false)
}
func (j *Journal) readMode(name string, headerOnly bool) (*journalState, error) {
	f, e := os.Open(filepath.Join(j.dir, name))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	s := &journalState{events: map[string]Event{}, delivered: map[string]bool{}, file: name, size: info.Size(), modified: info.ModTime()}
	for s.size-s.offset >= journalLengthSize {
		var header [journalLengthSize]byte
		if _, e = f.ReadAt(header[:], s.offset); e != nil {
			return nil, e
		}
		n := int64(binary.BigEndian.Uint32(header[:]))
		if n == 0 {
			break
		}
		if n < journalCipherOverhead || n > s.size-s.offset-journalLengthSize {
			return nil, fmt.Errorf("corrupt journal frame %s at %d", name, s.offset)
		}
		encrypted := make([]byte, n)
		if _, e = f.ReadAt(encrypted, s.offset+journalLengthSize); e != nil {
			return nil, e
		}
		plain, e := j.crypto.open("journal-v1", encrypted, []byte(name))
		if e != nil {
			return nil, fmt.Errorf("authenticate journal %s: %w", name, e)
		}
		var frame journalFrame
		if e = json.Unmarshal(plain, &frame); e != nil {
			return nil, e
		}
		switch frame.Type {
		case "admit":
			s.version = frame.Version
			s.Owner = frame.Owner
			s.RequestID = frame.RequestID
			s.State = "executing_or_ambiguous"
			s.AdmittedAt = frame.At
		case "event":
			if frame.Event == nil {
				return nil, errors.New("missing journal event")
			}
			s.events[frame.Event.UID] = *frame.Event
		case "delivered":
			s.delivered[frame.UID] = true
		case "terminal":
			s.State = AdmissionTerminal
		case AdmissionOutcomeUnknown:
			s.State = AdmissionOutcomeUnknown
		default:
			return nil, errors.New("unknown journal frame")
		}
		s.offset += journalLengthSize + n
		if headerOnly {
			break
		}
	}
	if s.Owner == "" || s.RequestID == "" {
		return nil, errors.New("journal admission missing")
	}
	for uid := range s.events {
		if !s.delivered[uid] {
			s.Pending++
			s.pendingControlBytes += journalDeliveryBytes(uid)
		}
	}
	return s, nil
}
func (j *Journal) journalEntries() ([]os.DirEntry, error) {
	entries, e := os.ReadDir(j.dir)
	if e != nil {
		return nil, e
	}
	out := entries[:0]
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".admitting-") {
			if !entry.Type().IsRegular() {
				return nil, errors.New("unsafe unpublished journal entry")
			}
			if e := os.Remove(filepath.Join(j.dir, entry.Name())); e != nil {
				return nil, e
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".wal") {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, errors.New("unsafe journal entry")
		}
		out = append(out, entry)
	}
	return out, nil
}
func (j *Journal) states() ([]*journalState, error) {
	entries, e := j.journalEntries()
	if e != nil {
		return nil, e
	}
	out := make([]*journalState, 0, len(entries))
	for _, entry := range entries {
		s, e := j.readMode(entry.Name(), true)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, nil
}
func journalDeliveryBytes(uid string) int64 {
	raw, _ := json.Marshal(journalFrame{Type: "delivered", UID: uid})
	return int64(len(raw)) + journalCipherOverhead + journalLengthSize
}

func (j *Journal) append(s *journalState, frame journalFrame) error {
	return j.appendBatchAt(s, []journalFrame{frame}, s.pendingControlBytes, s.file)
}

func (j *Journal) appendBatchAt(s *journalState, frames []journalFrame, reserve int64, path string) error {
	if len(frames) == 0 {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(j.dir, path), os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	// Leave the first length at zero while staging the whole batch. The first
	// length publishes the batch only after all frames and the new end marker
	// are durable. A shorter retry hides the tail of any uncommitted batch.
	var firstLength, header [journalLengthSize]byte
	offset, size := s.offset, s.size
	for i, frame := range frames {
		raw, err := json.Marshal(frame)
		if err != nil {
			return err
		}
		if int64(len(raw)) > math.MaxUint32-journalCipherOverhead {
			return errors.New("durable event exceeds the journal frame format")
		}
		encrypted, err := j.crypto.seal("journal-v1", raw, []byte(s.file))
		if err != nil {
			return err
		}
		frameBytes := journalLengthSize + int64(len(encrypted))
		if frameBytes > math.MaxInt64-offset-journalLengthSize-reserve {
			return ErrOverloaded
		}
		needed := offset + frameBytes + journalLengthSize + reserve
		if needed > size {
			target := needed
			if size <= math.MaxInt64-journalGrowthBytes {
				target = max(target, size+journalGrowthBytes)
			}
			if err := preallocateJournal(f, size, target); err != nil {
				if target == needed || !isJournalSpaceError(err) {
					return err
				}
				if err := preallocateJournal(f, size, needed); err != nil {
					return err
				}
				target = needed
			}
			size = target
		}
		binary.BigEndian.PutUint32(header[:], uint32(len(encrypted)))
		if i == 0 {
			firstLength = header
		} else if _, err := f.WriteAt(header[:], offset); err != nil {
			return err
		}
		if _, err := f.WriteAt(encrypted, offset+journalLengthSize); err != nil {
			return err
		}
		offset += frameBytes
	}
	var end [journalLengthSize]byte
	if _, err := f.WriteAt(end[:], offset); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if _, err := f.WriteAt(firstLength[:], s.offset); err != nil {
		return err
	}
	return f.Sync()
}

func isJournalSpaceError(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}
func (j *Journal) syncDir() error {
	f, e := os.Open(j.dir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func (j *Journal) Admit(ctx context.Context, owner, request string) error {
	_, err := j.admit(ctx, owner, request, false)
	return err
}

// AdmitActive holds an exclusive lock on the reservation through request
// execution, including across replicas sharing the journal volume.
func (j *Journal) AdmitActive(ctx context.Context, owner, request string) (func(), error) {
	return j.admit(ctx, owner, request, true)
}

func (j *Journal) admit(ctx context.Context, owner, request string, active bool) (func(), error) {
	if owner == "" || request == "" {
		return nil, errors.New("admission owner and request identity required")
	}
	release, e := backupgate.HoldWorkUnit(ctx, "observability.admit")
	if e != nil {
		return nil, e
	}
	defer release()
	var releaseActive func()
	err := j.locked(ctx, func() error {
		if _, e := j.journalEntries(); e != nil {
			return e
		}
		name := j.key(owner, request)
		path := filepath.Join(j.dir, name)
		if _, e := os.Lstat(path); e == nil {
			return ErrRecoveryRequired
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		tmp, e := os.CreateTemp(j.dir, ".admitting-")
		if e != nil {
			return e
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName)
		defer func() {
			if releaseActive == nil {
				tmp.Close()
			}
		}()
		// Actually allocate terminal capacity before external execution. Truncate alone
		// would create a sparse promise that can fail when the accepted call completes.
		if e = preallocateJournal(tmp, 0, j.limits.RequestBytes); e != nil {
			return e
		}
		if e = tmp.Sync(); e != nil {
			return e
		}
		if active {
			if e = syscall.Flock(int(tmp.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
				return e
			}
		}
		s := &journalState{file: name, size: j.limits.RequestBytes}
		frame := journalFrame{Type: "admit", Version: 3, Owner: owner, RequestID: request, At: time.Now().UTC()}
		if e = j.appendBatchAt(s, []journalFrame{frame}, journalTerminalRoom, filepath.Base(tmpName)); e != nil {
			return e
		}
		if e = os.Rename(tmpName, path); e != nil {
			return e
		}
		if e = j.syncDir(); e != nil {
			return e
		}
		if active {
			releaseActive = func() { _ = tmp.Close() }
		}
		return nil
	})
	return releaseActive, err
}
func (j *Journal) Append(ctx context.Context, event Event) error {
	return j.AppendBatch(ctx, []Event{event})
}

// AppendBatch publishes the new events for a single admission together.
func (j *Journal) AppendBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	if err := validateJournalEventBatch(events); err != nil {
		return err
	}
	for _, event := range events {
		if event.UID == "" || event.Kind == "" || !json.Valid(event.Payload) {
			return errors.New("invalid durable event")
		}
	}
	return j.locked(ctx, func() error {
		s, err := j.read(j.key(events[0].Owner, events[0].RequestID))
		if err != nil {
			return err
		}
		frames := make([]journalFrame, 0, len(events))
		reserve := s.pendingControlBytes + journalTerminalRoom
		for i := range events {
			event := &events[i]
			if prior, ok := s.events[event.UID]; ok {
				if prior.Kind != event.Kind || !bytes.Equal(canonicalEventPayload(prior.Payload, event.Kind), canonicalEventPayload(event.Payload, event.Kind)) {
					return ErrIdentityConflict
				}
				continue
			}
			if s.isTerminal() {
				return errors.New("admission already terminal")
			}
			s.events[event.UID] = *event
			if !s.delivered[event.UID] {
				reserve += journalDeliveryBytes(event.UID)
			}
			frames = append(frames, journalFrame{Type: "event", Event: event})
		}
		return j.appendBatchAt(s, frames, reserve, s.file)
	})
}

func validateJournalEventBatch(events []Event) error {
	first := events[0]
	for _, event := range events[1:] {
		if event.Owner != first.Owner || event.RequestID != first.RequestID {
			return errors.New("durable event batch requires one owner and request")
		}
	}
	return nil
}

func (j *Journal) Finish(ctx context.Context, owner, request string) error {
	return j.locked(ctx, func() error {
		s, e := j.read(j.key(owner, request))
		if e != nil {
			return e
		}
		if s.isTerminal() {
			return nil
		}
		return j.append(s, journalFrame{Type: AdmissionTerminal})
	})
}
func (j *Journal) Delivered(ctx context.Context, event Event) error {
	return j.DeliveredBatch(ctx, []Event{event})
}

// DeliveredBatch acknowledges successful deliveries for a single admission.
func (j *Journal) DeliveredBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	if err := validateJournalEventBatch(events); err != nil {
		return err
	}
	return j.locked(ctx, func() error {
		s, e := j.read(j.key(events[0].Owner, events[0].RequestID))
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		frames := make([]journalFrame, 0, len(events))
		reserve := s.pendingControlBytes
		if !s.isTerminal() {
			reserve += journalTerminalRoom
		}
		for _, event := range events {
			if s.delivered[event.UID] {
				continue
			}
			s.delivered[event.UID] = true
			if _, accepted := s.events[event.UID]; accepted {
				reserve -= journalDeliveryBytes(event.UID)
			}
			frames = append(frames, journalFrame{Type: "delivered", UID: event.UID})
		}
		return j.appendBatchAt(s, frames, reserve, s.file)
	})
}
func (j *Journal) Recovery(ctx context.Context) ([]RecoveryState, error) {
	var out []RecoveryState
	e := j.locked(ctx, func() error {
		states, e := j.states()
		if e != nil {
			return e
		}
		for _, s := range states {
			current, e := j.read(s.file)
			if e != nil {
				return e
			}
			out = append(out, current.RecoveryState)
		}
		return nil
	})
	return out, e
}
func (j *Journal) Replay(ctx context.Context, limit int, sink func(context.Context, Event) error) (int, error) {
	return j.ReplayWithTerminal(ctx, limit, sink, nil)
}
func (j *Journal) ReplayWithTerminal(ctx context.Context, limit int, sink func(context.Context, Event) error, terminal func(context.Context, RecoveryState) error) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	release, e := backupgate.HoldWorkUnit(ctx, "observability.replay")
	if e != nil {
		return 0, e
	}
	defer release()
	var states []*journalState
	if e = j.locked(ctx, func() error { var err error; states, err = j.states(); return err }); e != nil {
		return 0, e
	}
	count := 0
	var conflict error
journals:
	for _, s := range states {
		if e = j.locked(ctx, func() error { var err error; s, err = j.read(s.file); return err }); errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return count, e
		}
		uids := make([]string, 0, len(s.events))
		for uid := range s.events {
			uids = append(uids, uid)
		}
		sort.Slice(uids, func(i, k int) bool {
			a, b := s.events[uids[i]], s.events[uids[k]]
			if a.Kind == "model_invocations" && b.Kind != "model_invocations" {
				return true
			}
			if b.Kind == "model_invocations" && a.Kind != "model_invocations" {
				return false
			}
			return uids[i] < uids[k]
		})
		delivered := make([]Event, 0, min(len(uids), limit-count))
		var sinkErr error
		for _, uid := range uids {
			if s.delivered[uid] {
				continue
			}
			if count+len(delivered) >= limit {
				break
			}
			event := s.events[uid]
			if sinkErr = sink(ctx, event); sinkErr != nil {
				break
			}
			delivered = append(delivered, event)
		}
		// Projection may have committed before the replay deadline. Acknowledge
		// those deliveries even when the original context can no longer write.
		ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		e = j.DeliveredBatch(ackCtx, delivered)
		cancel()
		if e != nil {
			return count, e
		}
		count += len(delivered)
		if errors.Is(sinkErr, ErrIdentityConflict) {
			conflict = sinkErr
			continue journals
		}
		if sinkErr != nil {
			return count, sinkErr
		}
		if e = j.locked(ctx, func() error {
			current, e := j.read(s.file)
			if errors.Is(e, os.ErrNotExist) {
				return nil
			}
			if e != nil {
				return e
			}
			if current.Pending != 0 {
				return nil
			}
			if !current.isTerminal() {
				now := time.Now()
				state := AdmissionTerminal
				if !current.hasLegacyStreamCompletion(now) {
					if terminal == nil {
						return nil
					}
					// Acknowledgments written by this replay are not new request
					// activity. A newly appended event starts a fresh idle period.
					if len(current.events) == len(s.events) {
						current.modified = s.modified
					}
					lock, err := j.lockAbandonedAdmission(current, now)
					if err != nil || lock == nil {
						return err
					}
					defer lock.Close()
					state = AdmissionOutcomeUnknown
				}
				if e = j.append(current, journalFrame{Type: state}); e != nil {
					return e
				}
				current.State = state
			}
			if current.State == AdmissionOutcomeUnknown && terminal == nil {
				return nil
			}
			if terminal != nil {
				if e = terminal(ctx, current.RecoveryState); e != nil {
					return e
				}
			}
			if e = os.Remove(filepath.Join(j.dir, s.file)); e != nil {
				return e
			}
			return j.syncDir()
		}); e != nil {
			return count, e
		}
		if count >= limit {
			return count, conflict
		}
	}
	return count, conflict
}

func (s *journalState) isTerminal() bool {
	return s.State == AdmissionTerminal || s.State == AdmissionOutcomeUnknown
}

func (j *Journal) lockAbandonedAdmission(s *journalState, now time.Time) (*os.File, error) {
	// Legacy writers did not hold the reservation lock. Only a still-empty
	// legacy reservation can be expired without a recorded stream end.
	if now.Sub(s.modified) < abandonedAdmissionAge || (s.version < 3 && len(s.events) > 0) {
		return nil, nil
	}
	file, err := os.OpenFile(filepath.Join(j.dir, s.file), os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, err
	}
	return file, nil
}

// Legacy transcript and cost conflicts prevented Finish even after the stream
// ended and its successful attempts were charged. Version 2 requires Finish.
func (s *journalState) hasLegacyStreamCompletion(now time.Time) bool {
	if s.version != 0 && s.version != 1 {
		return false
	}
	ended := false
	completed := make([]string, 0, 1)
	costs := map[string]bool{}
	for _, event := range s.events {
		if event.Owner != s.Owner || event.RequestID != s.RequestID {
			continue
		}
		var proof struct {
			Type               string    `json:"event_type"`
			RequestID          string    `json:"request_id"`
			UserID             int       `json:"user_id"`
			CreatedAt          time.Time `json:"created_at"`
			Status             string    `json:"status"`
			InvocationEventUID string    `json:"invocation_event_uid"`
		}
		switch event.Kind {
		case "audit_events", "model_invocations", "cost_records":
			if json.Unmarshal(event.Payload, &proof) != nil || proof.RequestID != s.RequestID ||
				proof.UserID <= 0 || s.Owner != "user:"+strconv.Itoa(proof.UserID) {
				continue
			}
		default:
			continue
		}
		switch event.Kind {
		case "audit_events":
			if proof.Type == "gateway.request.stream_failed" && !proof.CreatedAt.IsZero() && now.Sub(proof.CreatedAt) >= time.Minute {
				ended = true
			}
		case "model_invocations":
			if proof.Status == "completed" {
				completed = append(completed, event.UID)
			}
		case "cost_records":
			costs[proof.InvocationEventUID] = true
		}
	}
	if !ended || len(completed) == 0 {
		return false
	}
	for _, uid := range completed {
		if !costs[uid] {
			return false
		}
	}
	return true
}

// Snapshot returns authenticated journal files for an already-held backup gate.
// The caller must include the stable key in the separately encrypted secret backup.
func (j *Journal) Snapshot(ctx context.Context) (map[string][]byte, error) {
	out := map[string][]byte{}
	e := j.locked(ctx, func() error {
		states, e := j.states()
		if e != nil {
			return e
		}
		total := int64(0)
		for _, s := range states {
			s, e = j.read(s.file)
			if e != nil {
				return e
			}
			total += s.offset
			if total > 64<<20 {
				return errors.New("journal snapshot exceeds 64 MiB; use streaming filesystem backup")
			}
			f, e := os.Open(filepath.Join(j.dir, s.file))
			if e != nil {
				return e
			}
			b, e := io.ReadAll(io.LimitReader(f, s.offset))
			f.Close()
			if e != nil {
				return e
			}
			out[s.file] = b
		}
		return nil
	})
	return out, e
}

func writeJournalReservation(file *os.File, start, end int64) error {
	chunk := make([]byte, 64<<10)
	for offset := start; offset < end; {
		n := min(int64(len(chunk)), end-offset)
		if _, e := file.WriteAt(chunk[:n], offset); e != nil {
			return e
		}
		offset += n
	}
	return nil
}

// CheckCompletionReserve allocates space for the next attempt's completion
// before external execution, including acknowledgment and terminal frames.
func (j *Journal) CheckCompletionReserve(ctx context.Context, owner, request string, requiredBytes int64, events int) error {
	return j.locked(ctx, func() error {
		s, e := j.read(j.key(owner, request))
		if e != nil {
			return e
		}
		if s.isTerminal() {
			return ErrRecoveryRequired
		}
		base := s.offset + journalLengthSize + s.pendingControlBytes + journalTerminalRoom
		if requiredBytes < 0 || events < 0 || requiredBytes > math.MaxInt64-base ||
			int64(events) > (math.MaxInt64-base-requiredBytes)/journalFutureAckRoom {
			return errors.New("invalid journal completion reservation")
		}
		needed := base + requiredBytes + int64(events)*journalFutureAckRoom
		f, e := os.OpenFile(filepath.Join(j.dir, s.file), os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		if needed > s.size {
			if e = preallocateJournal(f, s.size, needed); e != nil {
				return fmt.Errorf("could not reserve completion space for the next provider attempt: %w", e)
			}
		}
		return f.Sync()
	})
}

// EventDigest matches receipts written before chat messages could recur in other requests.
func EventDigest(raw json.RawMessage) string {
	return digestEventPayload(raw, "")
}

func (event Event) ReceiptDigest() string {
	return digestEventPayload(event.Payload, event.Kind)
}

func digestEventPayload(raw json.RawMessage, kind string) string {
	hash := sha256.Sum256(canonicalEventPayload(raw, kind))
	return hex.EncodeToString(hash[:])
}

func canonicalEventPayload(raw json.RawMessage, kind string) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil {
		for _, field := range []string{"id", "created_at", "recorded_at", "completed_at"} {
			delete(fields, field)
		}
		// A transcript position belongs to the trace and can appear in several
		// calls. Keep its original attribution on the stored message.
		if kind == "chat_messages" {
			delete(fields, "request_id")
			delete(fields, "invocation_id")
		}
		raw, _ = json.Marshal(fields)
	}
	return raw
}
