package aigateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

type imageTestArtifacts struct {
	saved     int
	fail      bool
	owner     services.ImageArtifactOwner
	afterSave func()
}

func (a *imageTestArtifacts) Save(_ context.Context, owner services.ImageArtifactOwner, _ int, _, _ string, data []byte) (string, error) {
	if a.fail {
		return "", errors.New("storage secret error")
	}
	a.saved++
	a.owner = owner
	if a.afterSave != nil {
		a.afterSave()
	}
	return "img_01234567890123456789012345678901", nil
}
func (*imageTestArtifacts) Load(context.Context, services.ImageArtifactOwner, string) (*services.ImageArtifact, error) {
	return nil, services.ErrImageArtifactUnavailable
}

func imageExecutionGateway(t *testing.T, handler http.HandlerFunc) (*service, *httptest.Server, *routingBudgetStore, ChatCompletionRequest) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	model := imageRoutingModel()
	price := 0.2
	key := "provider-test-secret"
	model.BaseURL = server.URL + "/v1"
	model.Currency = "USD"
	model.ImagePricePerImage = &price
	model.APIKey = &key
	s := routingGateway([]models.LLMModel{model}, &stubAuditEventService{})
	s.modelAccessChecker = &accessTestChecker{}
	s.imageArtifacts = &imageTestArtifacts{}
	s.costRecordService = &routingCostService{}
	s.invocationService = &permissionInvocationRecorder{}
	s.httpClient = server.Client()
	s.deploymentStore = services.NewNoopGatewayDeploymentStore()
	budget := &routingBudgetStore{}
	s.imageJobs = newImageTestJobs(budget)
	s.imageJobs.(*imageTestJobs).invocations = s.invocationService
	s.imageJobs.(*imageTestJobs).costs = s.costRecordService
	keyID := 7
	instanceID := 8
	attr := ChatCompletionRequest{GatewayKeyID: &keyID, InstanceID: &instanceID, BudgetController: services.NewGatewayBudgetController(budget, keyID)}
	return s, server, budget, attr
}
func imageExecutionRequest(t *testing.T, extra string) *ImageOperationRequest {
	t.Helper()
	req, err := ParseImageOperationRequest(ImageGenerate, "application/json", strings.NewReader(`{"model":"image-only","prompt":"private prompt canary"`+extra+`}`))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestExecuteImageSuccessGovernanceAndRedaction(t *testing.T) {
	png := visionTestPNG(t)
	calls := 0
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer provider-test-secret" {
			t.Error("incorrect endpoint/auth")
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "native-image" || body["n"] != float64(2) {
			t.Error("alias/count not normalized", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"created": 1, "data": []map[string]string{{"b64_json": png}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 20, "total_tokens": 30}})
	})
	finalTokens := -1
	attr.UsageFinalizer = func(n int) error { finalTokens = n; return nil }
	result, err := s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, `,"n":2`), attr)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(result.Data) != 1 || result.Data[0].ArtifactID == "" || budget.reserved != 0.4 || budget.finalized != 0.2 || finalTokens != 30 {
		t.Fatal("governance mismatch", calls, result, budget, finalTokens)
	}
	store := s.imageArtifacts.(*imageTestArtifacts)
	if store.saved != 1 || store.owner.UserID != 1 || *store.owner.InstanceID != 8 || *store.owner.GatewayKeyID != 7 {
		t.Fatal("ownership lost", store)
	}
	cost := s.costRecordService.(*routingCostService).records[0]
	if cost.EstimatedCost != 0.2 || cost.ActualCost != nil || cost.InputUnitPrice != 0 || cost.OutputUnitPrice != 0 {
		t.Fatal("image charge misreported as token price/invoice", cost)
	}
	invocations := s.invocationService.(*permissionInvocationRecorder).items
	if len(invocations) != 1 || invocations[0].Status != "completed" || invocations[0].TrafficClass != "llm_image" {
		t.Fatal(invocations)
	}
	audit, _ := json.Marshal([]interface{}{s.auditEventService.(*stubAuditEventService).events, invocations, cost})
	for _, secret := range []string{"private prompt canary", png, "provider-test-secret"} {
		if strings.Contains(string(audit), secret) {
			t.Fatal("sensitive content in audit")
		}
	}
}

func TestExecuteImageRejectsBeforeUpstream(t *testing.T) {
	for _, name := range []string{"missing-price", "owner", "key-group", "capability", "stream", "url-output", "no-storage", "no-checker", "budget", "risk", "revoked"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			s, _, budget, attr := imageExecutionGateway(t, func(http.ResponseWriter, *http.Request) { calls++ })
			req := imageExecutionRequest(t, "")
			model := &s.modelRepo.(*stubLLMModelRepository).active[0]
			switch name {
			case "missing-price":
				model.ImagePricePerImage = nil
			case "owner":
				model.VisibilityScope = "selected_users"
				model.AllowedUserIDs = []int{2}
			case "key-group":
				attr.AllowedModelGroups = []string{"unrelated"}
			case "capability":
				model.ImageGenerationEnabled = false
			case "stream":
				req = imageExecutionRequest(t, `,"stream":true`)
			case "url-output":
				req = imageExecutionRequest(t, `,"response_format":"url"`)
			case "no-storage":
				s.imageArtifacts = nil
			case "no-checker":
				s.modelAccessChecker = nil
			case "budget":
				attr.BudgetController = services.NewGatewayBudgetController(&imageDeniedBudget{}, 7)
			case "risk":
				s.riskDetector = imageRiskBlock{}
			case "revoked":
				s.modelAccessChecker.(*accessTestChecker).revoked.Store(true)
			}
			if _, err := s.ExecuteImage(t.Context(), 1, req, attr); err == nil || calls != 0 || budget.finalized != 0 {
				t.Fatal("preflight bypass", err, calls, budget)
			}
		})
	}
}

type imageDeniedBudget struct{ routingBudgetStore }

func (*imageDeniedBudget) Reserve(context.Context, int, float64) (*services.GatewayBudgetLease, error) {
	return nil, services.ErrGatewayKeyBudgetExceeded
}

type imageRiskBlock struct{}

func (imageRiskBlock) AnalyzeText(string) services.RiskAnalysis {
	return services.RiskAnalysis{HighestAction: models.RiskActionBlock}
}

func TestExecuteImageFailureNoRetryAndBudgetOutcome(t *testing.T) {
	png := visionTestPNG(t)
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		charge      float64
		storageFail bool
	}{
		{"rejected", 403, `{"error":"secret-key https://signed-url/"}`, 0, false},
		{"upstream-error", 503, `{"error":"secret-key"}`, 0.2, false},
		{"invalid-json", 200, `not JSON secret-key`, 0.2, false},
		{"url-result", 200, `{"data":[{"url":"https://signed-url/"}]}`, 0.2, false},
		{"invalid-base64", 200, `{"data":[{"b64_json":"bad"}]}`, 0.2, false},
		{"empty", 200, `{"data":[]}`, 0.2, false},
		{"storage", 200, `{"data":[{"b64_json":"` + png + `"}]}`, 0.2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			s.imageArtifacts.(*imageTestArtifacts).fail = tc.storageFail
			got, err := s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, ""), attr)
			if err == nil || got != nil || calls != 1 || budget.finalized != tc.charge {
				t.Fatal("unexpected failure accounting", got, err, calls, budget)
			}
			var traced *ImageRequestError
			if !errors.As(err, &traced) || traced.RequestID == "" {
				t.Fatal("failure missing correlation ID", err)
			}
			invocations := s.invocationService.(*permissionInvocationRecorder).items
			if invocations[0].RequestID != traced.RequestID || !strings.Contains(*invocations[0].ResponsePayload, `"provider_status":`) {
				t.Fatal("missing safe provider diagnosis")
			}
			stored, _ := json.Marshal(s.invocationService.(*permissionInvocationRecorder).items)
			for _, secret := range []string{"secret-key", "signed-url", png} {
				if strings.Contains(err.Error()+string(stored), secret) {
					t.Fatal("provider secret leaked")
				}
			}
		})
	}
}

func TestExecuteImageRedirectAndUnknownUsage(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	s, _, _, attr := imageExecutionGateway(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	if _, err := s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, ""), attr); err == nil || destinationCalls.Load() != 0 {
		t.Fatal("redirect followed", err)
	}
	png := visionTestPNG(t)
	s, _, _, attr = imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+png+`"}],"usage":{}}`)
	})
	req := imageExecutionRequest(t, "")
	tokens := 0
	attr.UsageFinalizer = func(n int) error { tokens = n; return nil }
	result, err := s.ExecuteImage(t.Context(), 1, req, attr)
	if err != nil || result.Usage != nil || tokens != EstimateImageRequestTokens(req) {
		t.Fatal("missing usage refunded or invented", err, tokens)
	}
}

func TestExecuteImageRevocationCancelsInFlightAndRetainsEstimate(t *testing.T) {
	started := make(chan struct{})
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(4 * time.Second):
		}
	})
	go func() { <-started; s.modelAccessChecker.(*accessTestChecker).revoked.Store(true) }()
	_, err := s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, ""), attr)
	if !errors.Is(err, models.ErrModelUnavailable) || budget.finalized != 0.2 {
		t.Fatal("revocation did not stop paid call safely", err, budget)
	}
}

func TestExecuteImageRevocationBeforeReturn(t *testing.T) {
	png := visionTestPNG(t)
	s, _, budget, attr := imageExecutionGateway(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+png+`"}]}`)
	})
	s.imageArtifacts.(*imageTestArtifacts).afterSave = func() { s.modelAccessChecker.(*accessTestChecker).revoked.Store(true) }
	result, err := s.ExecuteImage(t.Context(), 1, imageExecutionRequest(t, ""), attr)
	if result != nil || !errors.Is(err, models.ErrModelUnavailable) || budget.finalized != 0.2 {
		t.Fatal("revoked result returned", result, err, budget)
	}
}

func TestExecuteImageEditJSONAndMultipart(t *testing.T) {
	png := visionTestPNG(t)
	for _, multipartInput := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "multipart"}[multipartInput], func(t *testing.T) {
			calls := 0
			s, _, _, attr := imageExecutionGateway(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/v1/images/edits" {
					t.Error(r.URL.Path)
				}
				if multipartInput {
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
					}
					if r.FormValue("model") != "native-image" {
						t.Error("alias not replaced")
					}
					file, _, err := r.FormFile("image")
					if err != nil {
						t.Error(err)
					} else {
						file.Close()
					}
				} else {
					var fields map[string]interface{}
					_ = json.NewDecoder(r.Body).Decode(&fields)
					if fields["images"] == nil {
						t.Error("lost image input")
					}
				}
				_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+png+`"}]}`)
			})
			s.modelRepo.(*stubLLMModelRepository).active[0].ImageEditEnabled = true
			body := []byte(`{"model":"image-only","prompt":"edit","images":[{"image_url":"data:image/png;base64,` + png + `"}]}`)
			contentType := "application/json"
			if multipartInput {
				data, _ := base64.StdEncoding.DecodeString(png)
				body, contentType = editMultipart(t, map[string][]string{"model": {"image-only"}, "prompt": {"edit"}}, [][2]string{{"image", string(data)}})
			}
			req, err := ParseImageOperationRequest(ImageEdit, contentType, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if result, err := s.ExecuteImage(t.Context(), 1, req, attr); err != nil || len(result.Data) != 1 || calls != 1 {
				t.Fatal(result, err, calls)
			}
		})
	}
}

type imageArtifactReadStore struct{ imageTestArtifacts }

func (*imageArtifactReadStore) Load(context.Context, services.ImageArtifactOwner, string) (*services.ImageArtifact, error) {
	return &services.ImageArtifact{ID: "artifact", ModelID: 100, MediaType: "image/png", Data: []byte("private")}, nil
}

type imageArtifactModelRepo struct {
	repository.LLMModelRepository
	model models.LLMModel
}

func (r *imageArtifactModelRepo) GetByID(int) (*models.LLMModel, error) { return &r.model, nil }

func TestImageDownloadRechecksModelAndUserAuthorization(t *testing.T) {
	for _, denial := range []string{"allowed", "revoked", "owner", "key-group", "inactive", "missing-checker"} {
		t.Run(denial, func(t *testing.T) {
			model := imageRoutingModel()
			checker := &accessTestChecker{}
			s := &service{imageArtifacts: &imageArtifactReadStore{}, modelRepo: &imageArtifactModelRepo{model: model}, modelAccessChecker: checker}
			var groups []string
			switch denial {
			case "revoked":
				checker.revoked.Store(true)
			case "owner":
				s.modelRepo.(*imageArtifactModelRepo).model.VisibilityScope = "selected_users"
				s.modelRepo.(*imageArtifactModelRepo).model.AllowedUserIDs = []int{2}
			case "key-group":
				groups = []string{"not-images"}
			case "inactive":
				s.modelRepo.(*imageArtifactModelRepo).model.IsActive = false
			case "missing-checker":
				s.modelAccessChecker = nil
			}
			got, err := s.LoadImageArtifact(t.Context(), services.ImageArtifactOwner{UserID: 1}, "artifact", groups)
			if denial == "allowed" {
				if err != nil || string(got.Data) != "private" {
					t.Fatal(err)
				}
			} else if !errors.Is(err, services.ErrImageArtifactUnavailable) || got != nil {
				t.Fatal("unauthorized image download", got, err)
			}
		})
	}
}
