package handlers

import (
	"clawreef/internal/aigateway"
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type classifierHandlerRepo struct {
	setting models.RoutingClassifierSetting
	saves   int
}

func (r *classifierHandlerRepo) Get(context.Context) (*models.RoutingClassifierSetting, error) {
	copy := r.setting
	if copy.TimeoutMS == 0 {
		copy.TimeoutMS = 5000
	}
	return &copy, nil
}
func (r *classifierHandlerRepo) Save(_ context.Context, setting *models.RoutingClassifierSetting) error {
	mode := r.setting.RoutingMode
	r.setting = *setting
	r.setting.RoutingMode = mode
	r.saves++
	return nil
}
func (r *classifierHandlerRepo) SaveMode(_ context.Context, mode string) error {
	r.setting.RoutingMode = mode
	r.saves++
	return nil
}
func classifierHandlerCall(fn gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/test", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	fn(ctx)
	return recorder
}
func TestRoutingClassifierHandlerSecretsAndComparison(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &classifierHandlerRepo{}
	h := NewRoutingClassifierHandler(aigateway.NewClassifierSettingsService(repo, strings.Repeat("a", 32)))
	response := classifierHandlerCall(h.Save, `{"base_url":"http://laya.example:8000","timeout_ms":5000,"api_key":"handler-test-secret"}`)
	if response.Code != 200 || strings.Contains(response.Body.String(), "handler-test-secret") || strings.Contains(response.Body.String(), "encrypted_api_key") {
		t.Fatal(response.Code, response.Body.String())
	}
	response = classifierHandlerCall(h.Get, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"has_api_key":true`) || strings.Contains(response.Body.String(), "handler-test-secret") {
		t.Fatal(response.Body.String())
	}
	response = classifierHandlerCall(h.Compare, `{"text":"你好","draft":{"base_url":"http://different.example:8000","timeout_ms":5000}}`)
	var decoded struct {
		Data aigateway.ClassifierComparison `json:"data"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &decoded)
	if response.Code != 200 || decoded.Data.Rule.Status != "success" || decoded.Data.Laya.ErrorCode != "key_required_for_new_url" || decoded.Data.ProductionRoutingChanged || repo.saves != 1 {
		t.Fatal(response.Body.String())
	}
}
func TestRoutingClassifierHandlerRejectsOversizedOrMalformedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &classifierHandlerRepo{}
	h := NewRoutingClassifierHandler(aigateway.NewClassifierSettingsService(repo, strings.Repeat("a", 32)))
	for _, fn := range []gin.HandlerFunc{h.Save, h.Compare, h.SetMode} {
		for _, body := range []string{`{`, strings.Repeat(" ", 70000) + `{}`} {
			response := classifierHandlerCall(fn, body)
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "syntax") {
				t.Fatal(response.Body.String())
			}
		}
	}
	if repo.saves != 0 {
		t.Fatal("invalid request saved")
	}
}

func TestRoutingClassifierModeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &classifierHandlerRepo{}
	h := NewRoutingClassifierHandler(aigateway.NewClassifierSettingsService(repo, strings.Repeat("a", 32)))
	for _, body := range []string{`{"mode":"invalid"}`, `{"mode":"hybrid"}`, `{"mode":"hybrid","confirm_external":true}`} {
		response := classifierHandlerCall(h.SetMode, body)
		if response.Code != 400 || repo.saves != 0 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	response := classifierHandlerCall(h.SetMode, `{"mode":"rules"}`)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"routing_mode":"rules"`) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = classifierHandlerCall(h.Save, `{"base_url":"http://laya.example:8000","timeout_ms":5000,"api_key":"handler-test-secret"}`)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	response = classifierHandlerCall(h.SetMode, `{"mode":"hybrid","confirm_external":true}`)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"mode":"hybrid_auto"`) || strings.Contains(response.Body.String(), "handler-test-secret") {
		t.Fatal(response.Code, response.Body.String())
	}
}
