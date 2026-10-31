package aigateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

// In-memory adapter for executor contract tests; SQL atomicity/recovery is
// tested separately against an opt-in disposable MySQL database.
type imageTestJobs struct {
	mu          sync.Mutex
	jobs        map[string]*services.ImageJob
	leases      map[string]*services.GatewayBudgetLease
	budget      services.GatewayBudgetService
	settleErr   error
	publishErr  error
	invocations services.ModelInvocationService
	costs       services.CostRecordService
}

func newImageTestJobs(budget services.GatewayBudgetService) *imageTestJobs {
	return &imageTestJobs{jobs: map[string]*services.ImageJob{}, leases: map[string]*services.GatewayBudgetLease{}, budget: budget}
}
func (s *imageTestJobs) Claim(_ context.Context, in *services.ImageJob) (*services.ImageJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.jobs {
		if job.ScopeHash == in.ScopeHash && job.IdempotencyHash == in.IdempotencyHash {
			if job.RequestHash != in.RequestHash {
				return nil, false, services.ErrImageIdempotencyConflict
			}
			copy := *job
			return &copy, false, nil
		}
	}
	copy := *in
	copy.State = "preparing"
	copy.CreatedAt = time.Now().UTC()
	copy.AnalyticsRecorded = in.AnalyticsContext == nil
	s.jobs[in.RequestID] = &copy
	return in, true, nil
}
func (s *imageTestJobs) FindIdempotent(_ context.Context, owner services.ImageArtifactOwner, key string) (*services.ImageJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.jobs {
		if job.ScopeHash == services.ImageJobScope(owner) && job.IdempotencyHash == services.ImageIdempotencyHash(key) {
			copy := *job
			return &copy, nil
		}
	}
	return nil, nil
}
func (s *imageTestJobs) Arm(_ context.Context, id string, lease *services.GatewayBudgetLease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs[id].State != "preparing" {
		return services.ErrImageJobUnavailable
	}
	s.jobs[id].State = "running"
	s.leases[id] = lease
	return nil
}
func (s *imageTestJobs) Finish(_ context.Context, id, state string, charge float64, result *string, code string, outcomes ...*services.ImageJobOutcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job.State != "preparing" && job.State != "running" {
		return services.ErrImageJobUnavailable
	}
	job.State, job.Charge, job.ResultMetadata, job.ErrorCode = state, charge, result, code
	if len(outcomes) > 0 && outcomes[0] != nil {
		raw, _ := json.Marshal(outcomes[0])
		text := string(raw)
		job.AnalyticsOutcome = &text
	}
	return nil
}
func (s *imageTestJobs) PublishRecords(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publishErr != nil {
		return s.publishErr
	}
	job := s.jobs[id]
	if job.AnalyticsRecorded {
		return nil
	}
	invocation, cost, err := services.BuildImageJobRecords(job)
	if err != nil {
		return err
	}
	if err := s.invocations.RecordInvocation(invocation); err != nil {
		return err
	}
	if cost != nil {
		if err := s.costs.RecordCost(cost); err != nil {
			return err
		}
	}
	job.AnalyticsRecorded = true
	return nil
}
func (s *imageTestJobs) Settle(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settleErr != nil {
		return s.settleErr
	}
	job := s.jobs[id]
	if job.Settled {
		return nil
	}
	if lease := s.leases[id]; lease != nil {
		if err := s.budget.Finalize(ctx, lease, job.Charge); err != nil {
			return err
		}
	}
	job.Settled = true
	return nil
}
func (s *imageTestJobs) Load(_ context.Context, owner services.ImageArtifactOwner, id string) (*services.ImageJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil || job.UserID != owner.UserID || (owner.InstanceID != nil && (job.InstanceID == nil || *owner.InstanceID != *job.InstanceID)) || (owner.GatewayKeyID != nil && (job.GatewayKeyID == nil || *owner.GatewayKeyID != *job.GatewayKeyID)) {
		return nil, services.ErrImageJobUnavailable
	}
	copy := *job
	return &copy, nil
}

type replayImageArtifacts struct {
	imageTestArtifacts
	data        []byte
	unavailable bool
	onLoad      func()
}

func (s *replayImageArtifacts) Load(_ context.Context, owner services.ImageArtifactOwner, id string) (*services.ImageArtifact, error) {
	if s.unavailable || owner.UserID != s.owner.UserID {
		return nil, services.ErrImageArtifactUnavailable
	}
	if s.onLoad != nil {
		s.onLoad()
	}
	return &services.ImageArtifact{ID: id, ModelID: imageRoutingModel().ID, MediaType: "image/png", Data: s.data}, nil
}

func TestImageIdempotencyReplayAndConflict(t *testing.T) {
	png := visionTestPNG(t)
	var calls atomic.Int32
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"created": 123, "data": []map[string]string{{"b64_json": png}}})
	})
	bytes, _ := base64.StdEncoding.DecodeString(png)
	artifacts := &replayImageArtifacts{data: bytes}
	s.imageArtifacts = artifacts
	s.modelRepo = &imageArtifactModelRepo{LLMModelRepository: s.modelRepo, model: s.modelRepo.(*stubLLMModelRepository).active[0]}
	attr.ImageIdempotencyKey = "one-logical-call"
	req := imageExecutionRequest(t, "")
	first, err := s.ExecuteImage(t.Context(), 1, req, attr)
	if err != nil {
		t.Fatal(err)
	}
	// Every HTTP retry has a fresh budget controller.
	attr.BudgetController = services.NewGatewayBudgetController(budget, 7)
	second, err := s.ExecuteImage(t.Context(), 1, req, attr)
	if err != nil || second.TraceID != first.TraceID || second.Created != 123 || second.Data[0].Base64 != png || calls.Load() != 1 || len(s.costRecordService.(*routingCostService).records) != 1 {
		t.Fatal("duplicate generation/accounting or lost result", second, err, calls.Load())
	}
	if _, err = s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, `,"n":2`), attr); !errors.Is(err, services.ErrImageIdempotencyConflict) || calls.Load() != 1 {
		t.Fatal("conflict dispatched", err)
	}
	artifacts.unavailable = true
	if _, err = s.ExecuteImage(t.Context(), 1, req, attr); err == nil || calls.Load() != 1 {
		t.Fatal("expired result regenerated", err)
	}
}

func TestImageConcurrentDuplicateDoesNotDispatch(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	png := visionTestPNG(t)
	var calls atomic.Int32
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"b64_json": png}}})
	})
	attr.ImageIdempotencyKey = "concurrent"
	req := imageExecutionRequest(t, "")
	done := make(chan error, 1)
	go func() { _, err := s.ExecuteImage(t.Context(), 1, req, attr); done <- err }()
	<-started
	duplicate := attr
	duplicate.BudgetController = services.NewGatewayBudgetController(budget, 7)
	_, err := s.ExecuteImage(t.Context(), 1, req, duplicate)
	var operationErr *ImageExecutionError
	if !errors.As(err, &operationErr) || operationErr.Code != "image_request_in_progress" || calls.Load() != 1 {
		t.Errorf("concurrent replay dispatched: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestImageUnknownOutcomeNeverResent(t *testing.T) {
	var calls atomic.Int32
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
	attr.ImageIdempotencyKey = "unknown"
	req := imageExecutionRequest(t, "")
	_, firstErr := s.ExecuteImage(t.Context(), 1, req, attr)
	attr.BudgetController = services.NewGatewayBudgetController(budget, 7)
	_, secondErr := s.ExecuteImage(t.Context(), 1, req, attr)
	var op *ImageExecutionError
	if firstErr == nil || !errors.As(secondErr, &op) || op.Code != "image_request_terminal" || calls.Load() != 1 || budget.finalized != .2 {
		t.Fatal(firstErr, secondErr, calls.Load(), budget)
	}
}

func TestImageSettlementRetryAndStatusAuthorization(t *testing.T) {
	png := visionTestPNG(t)
	var calls atomic.Int32
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"b64_json": png}}})
	})
	bytes, _ := base64.StdEncoding.DecodeString(png)
	s.imageArtifacts = &replayImageArtifacts{data: bytes}
	s.modelRepo = &imageArtifactModelRepo{LLMModelRepository: s.modelRepo, model: s.modelRepo.(*stubLLMModelRepository).active[0]}
	jobs := s.imageJobs.(*imageTestJobs)
	jobs.settleErr = errors.New("db unavailable")
	attr.ImageIdempotencyKey = "settlement"
	_, err := s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, ""), attr)
	var requestErr *ImageRequestError
	if !errors.As(err, &requestErr) {
		t.Fatal(err)
	}
	owner := services.ImageArtifactOwner{UserID: 1, InstanceID: attr.InstanceID, GatewayKeyID: attr.GatewayKeyID}
	status, err := s.ImageRequestStatus(t.Context(), owner, requestErr.RequestID, nil)
	if err != nil || status.State != "completed" || status.Settled || len(status.ArtifactIDs) != 1 {
		t.Fatal(status, err)
	}
	otherKey := 999
	for _, denied := range []services.ImageArtifactOwner{{UserID: 2}, {UserID: 1, GatewayKeyID: &otherKey}, {UserID: 1, InstanceID: &otherKey}} {
		if _, err := s.ImageRequestStatus(t.Context(), denied, requestErr.RequestID, nil); err == nil {
			t.Fatal("cross-owner/key status exposed")
		}
	}
	jobs.settleErr = nil
	attr.BudgetController = services.NewGatewayBudgetController(budget, 7)
	if _, err = s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, ""), attr); err != nil || calls.Load() != 1 || budget.finalized != .2 {
		t.Fatal("failed settlement not recovered", err, calls.Load(), budget)
	}
	s.modelAccessChecker.(*accessTestChecker).revoked.Store(true)
	if _, err := s.ImageRequestStatus(t.Context(), owner, requestErr.RequestID, nil); err == nil {
		t.Fatal("revoked model status exposed")
	}
	if _, err := s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, ""), attr); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("revoked replay exposed", err)
	}
}

func TestImageFingerprintCanonicalJSON(t *testing.T) {
	parse := func(raw string) *ImageOperationRequest {
		r, err := ParseImageOperationRequest(ImageEdit, "application/json", strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a := parse(`{"model":"img","prompt":"draw","images":[{"image_url":"https://example.com/a.png"}]}`)
	b := parse(`{ "images": [ { "image_url": "https://example.com/a.png" } ], "n":1, "stream":false, "prompt":"draw", "model":"img" }`)
	if a.fingerprint(ChatCompletionRequest{}) != b.fingerprint(ChatCompletionRequest{}) {
		t.Fatal("equivalent requests differ")
	}
	b.prompt = "different"
	b.fields["prompt"] = json.RawMessage(`"different"`)
	if a.fingerprint(ChatCompletionRequest{}) == b.fingerprint(ChatCompletionRequest{}) {
		t.Fatal("different prompts collide")
	}
	for _, key := range []string{"has space", "line\nbreak", "中文", strings.Repeat("x", 201)} {
		if ValidateImageIdempotencyKey(key) {
			t.Fatal("invalid key allowed")
		}
	}
}

func TestImageAnalyticsFailureReplaysWithoutPaidResubmission(t *testing.T) {
	png := visionTestPNG(t)
	var calls atomic.Int32
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"b64_json": png}}})
	})
	bytes, _ := base64.StdEncoding.DecodeString(png)
	s.imageArtifacts = &replayImageArtifacts{data: bytes}
	s.modelRepo = &imageArtifactModelRepo{LLMModelRepository: s.modelRepo, model: s.modelRepo.(*stubLLMModelRepository).active[0]}
	jobs := s.imageJobs.(*imageTestJobs)
	jobs.publishErr = errors.New("temporary analytics sink failure")
	attr.ImageIdempotencyKey = "analytics-retry"
	req := imageExecutionRequest(t, "")
	_, err := s.ExecuteImage(t.Context(), 1, req, attr)
	var traced *ImageRequestError
	var op *ImageExecutionError
	if !errors.As(err, &traced) || !errors.As(err, &op) || op.Code != "image_record_pending" {
		t.Fatal(err)
	}
	owner := services.ImageArtifactOwner{UserID: 1, InstanceID: attr.InstanceID, GatewayKeyID: attr.GatewayKeyID}
	status, err := s.ImageRequestStatus(t.Context(), owner, traced.RequestID, nil)
	if err != nil || status.State != "completed" || !status.Settled || status.RecordsReady == nil || *status.RecordsReady {
		t.Fatal(status, err)
	}
	job := jobs.jobs[traced.RequestID]
	for _, secret := range []string{"private prompt canary", png, "provider-test-secret"} {
		if strings.Contains(*job.AnalyticsContext, secret) || strings.Contains(*job.AnalyticsOutcome, secret) {
			t.Fatal("outbox contains private data")
		}
	}
	jobs.publishErr = nil
	attr.BudgetController = services.NewGatewayBudgetController(budget, 7)
	result, err := s.ExecuteImage(t.Context(), 1, req, attr)
	if err != nil || result.TraceID != traced.RequestID || calls.Load() != 1 || budget.finalized != .2 {
		t.Fatal("analytics retry regenerated/charged", result, err, calls.Load())
	}
	status, err = s.ImageRequestStatus(t.Context(), owner, traced.RequestID, nil)
	if err != nil || status.RecordsReady == nil || !*status.RecordsReady {
		t.Fatal(status, err)
	}
	if len(s.costRecordService.(*routingCostService).records) != 1 || len(s.invocationService.(*permissionInvocationRecorder).items) != 1 {
		t.Fatal("records missing or duplicated")
	}
	attr.BudgetController = services.NewGatewayBudgetController(budget, 7)
	if _, err := s.ExecuteImage(t.Context(), 1, req, attr); err != nil || calls.Load() != 1 || len(s.costRecordService.(*routingCostService).records) != 1 {
		t.Fatal("second replay duplicated", err)
	}
}

type recoverableImageArtifacts struct {
	imageTestArtifacts
	ids []string
}

func (s *recoverableImageArtifacts) ListForRequest(context.Context, services.ImageArtifactOwner, string) ([]string, error) {
	return s.ids, nil
}

func TestImageStatusRecoversPartialArtifactsWithoutRegeneration(t *testing.T) {
	var calls atomic.Int32
	s, _, _, attr := imageExecutionGateway(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	model := s.modelRepo.(*stubLLMModelRepository).active[0]
	s.modelRepo = &imageArtifactModelRepo{LLMModelRepository: s.modelRepo, model: model}
	store := &recoverableImageArtifacts{ids: []string{"img_saved_before_crash"}}
	s.imageArtifacts = store
	owner := services.ImageArtifactOwner{UserID: 1, InstanceID: attr.InstanceID, GatewayKeyID: attr.GatewayKeyID}
	id := "image_01234567890123456789012345678901"
	_, _, err := s.imageJobs.Claim(t.Context(), &services.ImageJob{RequestID: id, UserID: 1, InstanceID: attr.InstanceID, GatewayKeyID: attr.GatewayKeyID, ModelID: model.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.imageJobs.Finish(t.Context(), id, "unknown", .2, nil, "image_outcome_unknown"); err != nil {
		t.Fatal(err)
	}
	status, err := s.ImageRequestStatus(t.Context(), owner, id, nil)
	if err != nil || status.State != "unknown" || len(status.ArtifactIDs) != 1 || calls.Load() != 0 {
		t.Fatal(status, err, calls.Load())
	}
	store.ids = nil
	status, err = s.ImageRequestStatus(t.Context(), owner, id, nil)
	if err != nil || len(status.ArtifactIDs) != 0 || status.ArtifactIDs == nil {
		t.Fatal("expired result not removed", status, err)
	}
}
