package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/upper/db/v4"
)

var ErrImageIdempotencyConflict = errors.New("image idempotency key was already used for a different request")
var ErrImageJobUnavailable = errors.New("image request is unavailable")

type ImageJob struct {
	RequestID         string    `db:"request_id" json:"request_id"`
	ScopeHash         string    `db:"scope_hash" json:"-"`
	IdempotencyHash   string    `db:"idempotency_hash" json:"-"`
	RequestHash       string    `db:"request_hash" json:"-"`
	UserID            int       `db:"user_id" json:"-"`
	InstanceID        *int      `db:"instance_id" json:"-"`
	GatewayKeyID      *int      `db:"gateway_key_id" json:"-"`
	ModelID           int       `db:"model_id" json:"-"`
	State             string    `db:"state" json:"state"`
	ReservationID     *string   `db:"reservation_id" json:"-"`
	Estimate          float64   `db:"estimate" json:"estimate"`
	Charge            float64   `db:"charge" json:"charge"`
	Settled           bool      `db:"settled" json:"settled"`
	ResultMetadata    *string   `db:"result_metadata" json:"-"`
	ErrorCode         string    `db:"error_code" json:"error_code,omitempty"`
	DeadlineAt        time.Time `db:"deadline_at" json:"-"`
	CreatedAt         time.Time `db:"created_at" json:"created_at"`
	UpdatedAt         time.Time `db:"updated_at" json:"updated_at"`
	AnalyticsContext  *string   `db:"analytics_context" json:"-"`
	AnalyticsOutcome  *string   `db:"analytics_outcome" json:"-"`
	AnalyticsRecorded bool      `db:"analytics_recorded" json:"-"`
}

type GatewayImageJobStore interface {
	Claim(context.Context, *ImageJob) (*ImageJob, bool, error)
	FindIdempotent(context.Context, ImageArtifactOwner, string) (*ImageJob, error)
	Arm(context.Context, string, *GatewayBudgetLease) error
	Finish(context.Context, string, string, float64, *string, string, ...*ImageJobOutcome) error
	Settle(context.Context, string) error
	PublishRecords(context.Context, string) error
	Load(context.Context, ImageArtifactOwner, string) (*ImageJob, error)
}

type gatewayImageJobService struct {
	sess   db.Session
	budget GatewayBudgetService
}

func NewGatewayImageJobService(sess db.Session, budget GatewayBudgetService) *gatewayImageJobService {
	return &gatewayImageJobService{sess: sess, budget: budget}
}

func ImageJobScope(owner ImageArtifactOwner) string {
	raw, _ := json.Marshal(owner)
	return imageJobHash(raw)
}
func ImageIdempotencyHash(key string) string { return imageJobHash([]byte(key)) }
func imageJobHash(raw []byte) string         { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func (s *gatewayImageJobService) Claim(ctx context.Context, in *ImageJob) (*ImageJob, bool, error) {
	if in == nil || in.UserID <= 0 || in.ModelID <= 0 || len(in.RequestID) > 64 || len(in.ScopeHash) != 64 || len(in.IdempotencyHash) != 64 || len(in.RequestHash) != 64 || math.IsNaN(in.Estimate) || math.IsInf(in.Estimate, 0) || in.Estimate < 0 {
		return nil, false, ErrImageJobUnavailable
	}
	now := time.Now().UTC()
	if in.AnalyticsContext != nil {
		if _, err := decodeImageAnalytics(*in.AnalyticsContext); err != nil {
			return nil, false, err
		}
	}
	// A unique constraint serializes concurrent duplicate submissions across pods.
	_, err := s.sess.SQL().ExecContext(ctx, `INSERT INTO gateway_image_jobs
	(request_id,scope_hash,idempotency_hash,request_hash,user_id,instance_id,gateway_key_id,model_id,state,estimate,deadline_at,created_at,updated_at,analytics_context,analytics_recorded)
	VALUES (?,?,?,?,?,?,?,?,'preparing',?,?,?,?,?,?) ON DUPLICATE KEY UPDATE request_id=request_id`, in.RequestID, in.ScopeHash, in.IdempotencyHash, in.RequestHash, in.UserID, in.InstanceID, in.GatewayKeyID, in.ModelID, in.Estimate, now.Add(5*time.Minute), now, now, in.AnalyticsContext, in.AnalyticsContext == nil)
	if err != nil {
		return nil, false, err
	}
	var job ImageJob
	err = s.sess.WithContext(ctx).Collection("gateway_image_jobs").Find(db.Cond{"scope_hash": in.ScopeHash, "idempotency_hash": in.IdempotencyHash}).One(&job)
	if err != nil {
		return nil, false, err
	}
	// The winner pins the physical deployment. Auto competitors can select
	// different candidates before this unique-key insert; the request hash (which
	// includes the requested model) must match, not the tentative deployment ID.
	if job.RequestHash != in.RequestHash {
		return nil, false, ErrImageIdempotencyConflict
	}
	return &job, job.RequestID == in.RequestID, nil
}

func (s *gatewayImageJobService) FindIdempotent(ctx context.Context, owner ImageArtifactOwner, key string) (*ImageJob, error) {
	if owner.UserID <= 0 || key == "" {
		return nil, ErrImageJobUnavailable
	}
	var job ImageJob
	err := s.sess.WithContext(ctx).Collection("gateway_image_jobs").Find(db.Cond{"scope_hash": ImageJobScope(owner), "idempotency_hash": ImageIdempotencyHash(key)}).One(&job)
	if errors.Is(err, db.ErrNoMoreRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// Arm and budget hold are atomic. A crash after this point is conservatively
// treated as unknown; neither lease expiry nor a budget-period reset refunds it.
func (s *gatewayImageJobService) Arm(ctx context.Context, id string, lease *GatewayBudgetLease) error {
	return s.sess.Tx(func(tx db.Session) error {
		var keyID *int
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT gateway_key_id FROM gateway_image_jobs WHERE request_id=? AND state='preparing' AND deadline_at>? FOR UPDATE", id, time.Now().UTC())
		if err != nil {
			return err
		}
		if err = row.Scan(&keyID); err != nil {
			return err
		}
		var reservationID *string
		if keyID != nil {
			if lease == nil || lease.GatewayKeyID != *keyID || lease.ReservationID == "" {
				return ErrImageJobUnavailable
			}
			result, err := tx.SQL().ExecContext(ctx, `UPDATE gateway_key_budget_reservations SET settlement_status='image_pending'
			WHERE reservation_id=? AND gateway_key_id=? AND settlement_status='reserved' AND expires_at>?`, lease.ReservationID, *keyID, time.Now().UTC())
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrImageJobUnavailable
			}
			reservationID = &lease.ReservationID
		} else if lease != nil {
			return ErrImageJobUnavailable
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE gateway_image_jobs SET state='running',reservation_id=?,deadline_at=?,updated_at=? WHERE request_id=?`, reservationID, time.Now().UTC().Add(5*time.Minute), time.Now().UTC(), id)
		return err
	})
}

func (s *gatewayImageJobService) Finish(ctx context.Context, id, state string, charge float64, result *string, code string, outcomes ...*ImageJobOutcome) error {
	if (state != "completed" && state != "failed" && state != "unknown") || charge < 0 || math.IsNaN(charge) || math.IsInf(charge, 0) || len(code) > 64 {
		return ErrImageJobUnavailable
	}
	if result != nil && len(*result) > 65536 {
		return ErrImageJobUnavailable
	}
	var outcomeText *string
	if len(outcomes) > 1 {
		return ErrImageJobUnavailable
	}
	if len(outcomes) == 1 && outcomes[0] != nil {
		raw, err := json.Marshal(outcomes[0])
		if err != nil || len(raw) > 65536 {
			return ErrImageJobUnavailable
		}
		text := string(raw)
		outcomeText = &text
	}
	r, err := s.sess.SQL().ExecContext(ctx, `UPDATE gateway_image_jobs SET state=?,charge=?,result_metadata=?,error_code=?,analytics_outcome=?,updated_at=?
	WHERE request_id=? AND state IN ('preparing','running')`, state, charge, result, code, outcomeText, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrImageJobUnavailable
	}
	return nil
}

func (s *gatewayImageJobService) get(ctx context.Context, id string) (*ImageJob, error) {
	var job ImageJob
	if err := s.sess.WithContext(ctx).Collection("gateway_image_jobs").Find(db.Cond{"request_id": id}).One(&job); err != nil {
		return nil, ErrImageJobUnavailable
	}
	return &job, nil
}
func (s *gatewayImageJobService) Load(ctx context.Context, owner ImageArtifactOwner, id string) (*ImageJob, error) {
	job, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if owner.UserID <= 0 || job.UserID != owner.UserID || (owner.InstanceID != nil && (job.InstanceID == nil || *job.InstanceID != *owner.InstanceID)) || (owner.GatewayKeyID != nil && (job.GatewayKeyID == nil || *job.GatewayKeyID != *owner.GatewayKeyID)) {
		return nil, ErrImageJobUnavailable
	}
	return job, nil
}

func (s *gatewayImageJobService) Settle(ctx context.Context, id string) error {
	job, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if job.Settled {
		return nil
	}
	if job.State == "preparing" || job.State == "running" {
		return ErrImageJobUnavailable
	}
	if job.ReservationID != nil {
		if s.budget == nil || job.GatewayKeyID == nil {
			return ErrImageJobUnavailable
		}
		lease := &GatewayBudgetLease{ReservationID: *job.ReservationID, GatewayKeyID: *job.GatewayKeyID, ReservedCost: job.Estimate}
		if err := s.budget.Finalize(ctx, lease, job.Charge); err != nil {
			return err
		}
	}
	_, err = s.sess.SQL().ExecContext(ctx, "UPDATE gateway_image_jobs SET settled=TRUE,updated_at=? WHERE request_id=?", time.Now().UTC(), id)
	return err
}

// Recovery NEVER resends a provider request. Terminal outcomes are immutable;
// repeated/concurrent workers settle the same durable reservation idempotently.
func (s *gatewayImageJobService) Recover(ctx context.Context) error {
	now := time.Now().UTC()
	_, err := s.sess.SQL().ExecContext(ctx, `UPDATE gateway_image_jobs SET state='unknown',charge=estimate,error_code='image_outcome_unknown',updated_at=? WHERE state='running' AND deadline_at<=?`, now, now)
	if err != nil {
		return err
	}
	_, err = s.sess.SQL().ExecContext(ctx, `UPDATE gateway_image_jobs SET state='failed',charge=0,error_code='image_preparation_interrupted',updated_at=? WHERE state='preparing' AND deadline_at<=?`, now, now)
	if err != nil {
		return err
	}
	var jobs []ImageJob
	if err = s.sess.WithContext(ctx).Collection("gateway_image_jobs").Find(db.Cond{"settled": false, "state IN": []string{"completed", "failed", "unknown"}}).OrderBy("updated_at", "request_id").Limit(100).All(&jobs); err != nil {
		return err
	}
	var failures int
	for _, job := range jobs {
		if s.Settle(ctx, job.RequestID) != nil {
			failures++
			// Rotate failed work behind other jobs, so one permanently broken
			// key does not starve all subsequent settlements.
			_, _ = s.sess.SQL().ExecContext(ctx, "UPDATE gateway_image_jobs SET updated_at=? WHERE request_id=? AND settled=FALSE", time.Now().UTC(), job.RequestID)
		}
	}
	// Independent of budget settlement: a completed image remains completed in
	// analytics even if the budget sink is temporarily unavailable (and vice versa).
	var pending []ImageJob
	if err := s.sess.WithContext(ctx).Collection("gateway_image_jobs").Find(db.Cond{"analytics_recorded": false, "state IN": []string{"completed", "failed", "unknown"}}).OrderBy("updated_at", "request_id").Limit(100).All(&pending); err != nil {
		return err
	}
	for _, job := range pending {
		if s.PublishRecords(ctx, job.RequestID) != nil {
			failures++
			_, _ = s.sess.SQL().ExecContext(ctx, "UPDATE gateway_image_jobs SET updated_at=? WHERE request_id=? AND analytics_recorded=FALSE", time.Now().UTC(), job.RequestID)
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d image job settlements or analytics writes remain pending", failures)
	}
	return nil
}
func (s *gatewayImageJobService) RunRecovery(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		recoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := s.Recover(recoveryCtx); err != nil {
			log.Print("Image job recovery has pending work; will retry")
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
