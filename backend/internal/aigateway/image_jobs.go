package aigateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

func WithImageJobs(store services.GatewayImageJobStore) ServiceOption {
	return func(s *service) { s.imageJobs = store }
}

// Canonical JSON ignores multipart boundaries and uploaded filenames, but
// retains semantic field values, image order/bytes, operation and runtime task.
func (r *ImageOperationRequest) fingerprint(attribution ChatCompletionRequest) string {
	fields := make(map[string]interface{}, len(r.fields))
	for key, value := range r.fields {
		var decoded interface{}
		decoder := json.NewDecoder(strings.NewReader(string(value)))
		decoder.UseNumber()
		if decoder.Decode(&decoded) == nil && decoded != nil {
			fields[key] = decoded
		}
	}
	fields["n"] = r.count
	fields["stream"] = r.streaming
	var uploads []map[string]string
	for _, upload := range r.uploads {
		sum := sha256.Sum256(upload.data)
		uploads = append(uploads, map[string]string{"field": upload.field, "sha256": hex.EncodeToString(sum[:])})
	}
	raw, _ := json.Marshal(struct {
		Operation ImageOperation
		Fields    map[string]interface{}
		Uploads   []map[string]string
		Task      *models.GatewayTaskIdentity
	}{r.operation, fields, uploads, attribution.RuntimeTask})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ValidateImageIdempotencyKey(key string) bool {
	if len(key) > 200 {
		return false
	}
	for _, ch := range key {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}

type savedImageResult struct {
	Created     int64       `json:"created"`
	ArtifactIDs []string    `json:"artifact_ids"`
	Usage       *imageUsage `json:"usage,omitempty"`
}

// Retry permission is for the durable winner, not a newly selected Auto model.
// Never re-route a paid/unknown task when its original model was revoked.
func (s *service) replayPinnedImageJob(ctx context.Context, owner services.ImageArtifactOwner, job *services.ImageJob, req *ImageOperationRequest, attr ChatCompletionRequest) (*ImageExecutionResponse, error) {
	if job.RequestHash != req.fingerprint(attr) {
		return nil, services.ErrImageIdempotencyConflict
	}
	model, err := repository.ForModelUser(s.modelRepo, owner.UserID).GetByID(job.ModelID)
	if err != nil {
		return nil, err
	}
	if model == nil || !model.IsActive || !modelAllowedForGatewayKey(model, attr.AllowedModelGroups) || req.ValidateModel(model) != nil {
		return nil, models.ErrModelUnavailable
	}
	ctx, stop, err := guardModelCall(ctx, s.taskAccessChecker(attr), owner.UserID, job.ModelID)
	if err != nil {
		return nil, err
	}
	defer stop()
	result, err := s.replayImageJob(ctx, owner, job, attr.AllowedModelGroups)
	if err == nil {
		err = s.taskAccessChecker(attr).CheckModelAccess(ctx, owner.UserID, job.ModelID)
	}
	if err == nil {
		err = context.Cause(ctx)
	}
	if err != nil {
		return nil, &ImageRequestError{RequestID: job.RequestID, Err: err}
	}
	result.Accounting["selected_model_id"] = job.ModelID
	// The display name may have changed since generation; report the original
	// snapshot, never mislabel an old result using today's model configuration.
	var snapshot services.ImageJobAnalytics
	if job.AnalyticsContext != nil && json.Unmarshal([]byte(*job.AnalyticsContext), &snapshot) == nil && snapshot.Version == 1 {
		result.Accounting["selected_model"] = snapshot.ModelName
	}
	result.Accounting["routing_policy"] = imageRoutingPolicy(req.model)
	return result, nil
}

func (s *service) replayImageJob(ctx context.Context, owner services.ImageArtifactOwner, job *services.ImageJob, groups []string) (*ImageExecutionResponse, error) {
	fail := func(code, message string) (*ImageExecutionResponse, error) {
		return nil, &ImageRequestError{RequestID: job.RequestID, Err: imageError(409, code, message)}
	}
	if job.State != "completed" {
		if job.State == "running" || job.State == "preparing" {
			return fail("image_request_in_progress", "This image request is already in progress; query its request ID instead of submitting a new job")
		}
		return fail("image_request_terminal", "This image request has already ended; inspect its status before creating a new request")
	}
	if !job.Settled {
		if err := s.imageJobs.Settle(ctx, job.RequestID); err != nil {
			return fail("image_settlement_pending", "The result exists but budget settlement is pending; do not create a new request")
		}
	}
	if err := s.imageJobs.PublishRecords(ctx, job.RequestID); err != nil {
		return fail("image_record_pending", "The result exists but analytics persistence is pending; do not create a new request")
	}
	var saved savedImageResult
	if job.ResultMetadata == nil || json.Unmarshal([]byte(*job.ResultMetadata), &saved) != nil || len(saved.ArtifactIDs) == 0 || len(saved.ArtifactIDs) > 10 {
		return fail("image_result_unavailable", "The saved image result is unavailable; no new generation was attempted")
	}
	result := &ImageExecutionResponse{Created: saved.Created, Usage: saved.Usage, TraceID: job.RequestID, Accounting: map[string]interface{}{"request_id": job.RequestID, "replayed": true, "estimated_cost": job.Charge, "price_basis": "admin_per_image_estimate", "currency": "USD", "artifact_retention_days": 7}}
	for _, id := range saved.ArtifactIDs {
		artifact, err := s.LoadImageArtifact(ctx, owner, id, groups)
		if err != nil {
			return fail("image_result_unavailable", "The image expired or is no longer available; no new generation was attempted")
		}
		result.Data = append(result.Data, imageResultItem{ArtifactID: id, Base64: base64.StdEncoding.EncodeToString(artifact.Data)})
	}
	if err := context.Cause(ctx); err != nil {
		return nil, &ImageRequestError{RequestID: job.RequestID, Err: err}
	}
	return result, nil
}

// Status exposes only safe result references, not credentials or request hashes.
type ImageJobStatus struct {
	RequestID    string   `json:"request_id"`
	State        string   `json:"state"`
	Settled      bool     `json:"settled"`
	RecordsReady *bool    `json:"records_ready,omitempty"`
	Charge       float64  `json:"estimated_cost"`
	ErrorCode    string   `json:"error_code,omitempty"`
	ArtifactIDs  []string `json:"artifact_ids"`
}

func (s *service) ImageRequestStatus(ctx context.Context, owner services.ImageArtifactOwner, id string, groups []string) (*ImageJobStatus, error) {
	if s.imageJobs == nil || !strings.HasPrefix(id, "image_") || len(id) != 38 {
		return nil, services.ErrImageJobUnavailable
	}
	job, err := s.imageJobs.Load(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	model, err := repository.ForModelUser(s.modelRepo, owner.UserID).GetByID(job.ModelID)
	if err != nil || model == nil || !model.IsActive || !modelAllowedForGatewayKey(model, groups) {
		return nil, services.ErrImageJobUnavailable
	}
	checker := s.modelAccessChecker
	if checker == nil {
		checker, _ = s.modelRepo.(repository.ModelAccessChecker)
	}
	if checker == nil || checker.CheckModelAccess(ctx, owner.UserID, job.ModelID) != nil {
		return nil, services.ErrImageJobUnavailable
	}
	status := &ImageJobStatus{RequestID: id, State: job.State, Settled: job.Settled, Charge: job.Charge, ErrorCode: job.ErrorCode, ArtifactIDs: []string{}}
	if job.AnalyticsContext != nil {
		ready := job.AnalyticsRecorded
		status.RecordsReady = &ready
	}
	var saved savedImageResult
	if job.ResultMetadata != nil && json.Unmarshal([]byte(*job.ResultMetadata), &saved) == nil {
		status.ArtifactIDs = saved.ArtifactIDs
	}
	if recoverable, ok := s.imageArtifacts.(interface {
		ListForRequest(context.Context, services.ImageArtifactOwner, string) ([]string, error)
	}); ok {
		ids, err := recoverable.ListForRequest(ctx, owner, id)
		if err != nil {
			return nil, err
		}
		// The artifact table is authoritative for availability, including
		// partial saves before a crash and the seven-day expiration policy.
		status.ArtifactIDs = ids
	}
	if status.ArtifactIDs == nil {
		status.ArtifactIDs = []string{}
	}
	if err := checker.CheckModelAccess(ctx, owner.UserID, job.ModelID); err != nil {
		return nil, services.ErrImageJobUnavailable
	}
	return status, nil
}
