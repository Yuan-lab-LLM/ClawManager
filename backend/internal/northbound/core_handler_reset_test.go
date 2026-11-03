package northbound

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireResetDataLossConfirmationFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{"", `{}`, `{"confirm_data_loss":false}`, `{not-json}`} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/reset", strings.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		if requireResetDataLossConfirmation(context) {
			t.Fatalf("body %q unexpectedly confirmed reset", body)
		}
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "RESET_CONFIRMATION_REQUIRED") {
			t.Fatalf("body %q status/response = %d/%s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestRequireResetDataLossConfirmationAcceptsExplicitTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/reset", strings.NewReader(`{"confirm_data_loss":true}`))
	context.Request.Header.Set("Content-Type", "application/json")
	if !requireResetDataLossConfirmation(context) {
		t.Fatalf("explicit confirmation was rejected: %d/%s", recorder.Code, recorder.Body.String())
	}
}

func TestRequireDeleteDataLossConfirmationFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{"", `{}`, `{"confirm_data_loss":false}`, `{not-json}`} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		if requireDeleteDataLossConfirmation(context) {
			t.Fatalf("body %q unexpectedly confirmed delete", body)
		}
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "DELETE_CONFIRMATION_REQUIRED") {
			t.Fatalf("body %q status/response = %d/%s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestRequireDeleteDataLossConfirmationAcceptsExplicitTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(`{"confirm_data_loss":true}`))
	context.Request.Header.Set("Content-Type", "application/json")
	if !requireDeleteDataLossConfirmation(context) {
		t.Fatalf("explicit delete confirmation was rejected: %d/%s", recorder.Code, recorder.Body.String())
	}
}
