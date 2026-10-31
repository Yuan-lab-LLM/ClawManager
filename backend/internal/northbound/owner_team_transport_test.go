package northbound

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

func TestOwnerTeamTransportStreamsFilesAndBoundsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	binary := bytes.Repeat([]byte("x"), 3<<20)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/upload"):
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("files")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if !bytes.Equal(data, binary) {
				t.Error("multipart changed")
			}
			io.WriteString(w, `{"success":true}`)
		case strings.HasSuffix(r.URL.Path, "/download"):
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="report.bin"`)
			w.Write(binary)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write(binary)
		}
	}))
	defer core.Close()
	base, _ := url.Parse(core.URL)
	client := &CoreClient{baseURL: base, client: core.Client(), internalSecret: strings.Repeat("s", 40)}
	router := gin.New()
	router.Use(BodyLimit(64 << 10))
	handle := func(c *gin.Context) {
		if err := client.Forward(c, Principal{UserID: 1, SessionID: "fixture", Scopes: []string{ScopeTeamFilesRead, ScopeTeamFilesWrite}}); err != nil {
			writeError(c, err)
		}
	}
	router.POST("/api/northbound/v1/teams/:id/workspace/upload", handle)
	router.GET("/api/northbound/v1/teams/:id/workspace/download", handle)
	router.GET("/api/northbound/v1/teams/:id", handle)
	router.POST("/api/northbound/v1/teams", handle)
	form := &bytes.Buffer{}
	writer := multipart.NewWriter(form)
	part, _ := writer.CreateFormFile("files", "中文.bin")
	part.Write(binary)
	writer.Close()
	request := httptest.NewRequest("POST", "/api/northbound/v1/teams/1/workspace/upload", form)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	out := httptest.NewRecorder()
	router.ServeHTTP(out, request)
	if out.Code != 200 {
		t.Fatalf("upload: %d %s", out.Code, out.Body.String())
	}
	out = httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest("GET", "/api/northbound/v1/teams/1/workspace/download", nil))
	if out.Code != 200 || !bytes.Equal(out.Body.Bytes(), binary) || out.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("binary stream failed")
	}
	out = httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest("GET", "/api/northbound/v1/teams/1", nil))
	if out.Code != 502 {
		t.Fatalf("large JSON silently truncated: %d", out.Code)
	}
	out = httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest("POST", "/api/northbound/v1/teams", bytes.NewReader(binary)))
	if out.Code == 200 {
		t.Fatal("ordinary body limit bypassed")
	}
}

func TestOwnerTeamEventsDoNotExposePrivateChannels(t *testing.T) {
	events := []services.TeamEventPayload{}
	for _, payload := range []map[string]interface{}{{"text": "analysis secret", "channel": "analysis"}, {"text": "hidden secret", "chatPolicy": "hidden"}, {"text": "visible result", "chatPolicy": "visible"}} {
		events = append(events, services.TeamEventPayload{TeamEvent: models.TeamEvent{EventType: "reply"}, Payload: payload})
	}
	got := ownerTeamVisibleEvents(events)
	if len(got) != 1 {
		t.Fatalf("private events escaped: %#v", got)
	}
}
