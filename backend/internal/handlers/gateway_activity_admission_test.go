package handlers

import (
	"clawreef/internal/repository"
	"context"
	"fmt"
	"github.com/upper/db/v4/adapter/mysql"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/aigateway"
	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type activityHTTPRecorder struct {
	begins, finishes int
	beginErr         error
}

func (s *activityHTTPRecorder) BeginModelActivity(context.Context, int, int, int, string) (string, error) {
	s.begins++
	return "scope", s.beginErr
}
func (s *activityHTTPRecorder) RenewModelActivity(context.Context, string) error { return nil }
func (s *activityHTTPRecorder) FinishModelActivity(context.Context, string) error {
	s.finishes++
	return nil
}

type deniedActivityLimiter struct {
	*fakeGatewayRateLimitStore
	calls int
	svc   *activityHTTPRecorder
	t     *testing.T
}

func (s *deniedActivityLimiter) Acquire(context.Context, services.GatewayRateLimitInput) (*services.GatewayRateLimitLease, error) {
	s.calls++
	if s.svc.begins != 1 {
		s.t.Error("limiter ran before durable model admission")
	}
	return nil, services.ErrGatewayKeyRPMLimitExceeded
}

func TestGatewayActivityAdmissionPrecedesRateLimitAllProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, entry := range []string{"chat", "responses", "anthropic", "images"} {
		for _, fail := range []bool{false, true} {
			t.Run(entry+map[bool]string{false: "-quota", true: "-admission-failure"}[fail], func(t *testing.T) {
				svc := &activityHTTPRecorder{}
				if fail {
					svc.beginErr = models.ErrActivityUnavailable
				}
				actual := aigateway.NewService(nil, nil, nil, nil, nil, nil, nil, nil, aigateway.WithModelActivity(svc))
				limiter := &deniedActivityLimiter{fakeGatewayRateLimitStore: &fakeGatewayRateLimitStore{}, svc: svc, t: t}
				h := &AIGatewayHandler{service: actual, rateLimitStore: limiter}

				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Set("userID", 7)
				c.Set("instanceID", 1)
				c.Set("gatewayKeyID", "key")
				body := `{"model":"auto","messages":[{"role":"user","content":"hello"}]}`
				if entry == "responses" {
					body = `{"model":"auto","input":"hello"}`
				}
				if entry == "anthropic" {
					body = `{"model":"auto","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`
				}
				if entry == "images" {
					body = `{"model":"image","prompt":"hello"}`
				}
				c.Request = httptest.NewRequest("POST", "/"+entry, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				switch entry {
				case "chat":
					h.ChatCompletions(c)
				case "responses":
					h.Responses(c)
				case "anthropic":
					h.AnthropicMessages(c)
				case "images":
					h.imageOperation(c, aigateway.ImageGenerate)
				}
				expected := 429
				if fail {
					expected = 503
				}
				if rec.Code != expected {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
				if svc.begins != 1 || svc.finishes != map[bool]int{false: 1, true: 0}[fail] || limiter.calls != map[bool]int{false: 1, true: 0}[fail] {
					t.Fatalf("begin=%d finish=%d limit=%d", svc.begins, svc.finishes, limiter.calls)
				}
			})
		}
	}
}

func TestGatewayActivityPublicProtocolsRejectStoppedAndStaleMySQL(t *testing.T) {
	port := os.Getenv("TEST_IDLE_MYSQL_PORT")
	if port == "" {
		t.Skip("disposable localhost MySQL required")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			t.Fatal("localhost port required")
		}
	}
	url := mysql.ConnectionURL{Host: "127.0.0.1:" + port, User: "root", Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("cm_idle_http_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	url.Database = name
	s, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); root.SQL().Exec("DROP DATABASE " + name); root.Close() })
	for _, q := range []string{`CREATE TABLE instances(id INT PRIMARY KEY,user_id INT,runtime_generation INT,instance_mode VARCHAR(16),status VARCHAR(32))`, `CREATE TABLE instance_runtime_bindings(instance_id INT,generation INT,gateway_id VARCHAR(128),state VARCHAR(32))`, `INSERT INTO instances VALUES(1,7,2,'lite','running')`, `INSERT INTO instance_runtime_bindings VALUES(1,2,'gw-1-2','running')`} {
		if _, err = s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../db/migrations/096_add_instance_auto_stop.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err = s.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	activity := repository.NewInstanceAutoStopRepository(s)
	if err = activity.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	actual := aigateway.NewService(nil, nil, nil, nil, nil, nil, nil, nil, aigateway.WithModelActivity(activity))
	for _, entry := range []string{"chat", "responses", "anthropic", "images"} {
		for _, scenario := range []string{"stopped", "stale"} {
			t.Run(entry+"-"+scenario, func(t *testing.T) {
				status := "running"
				gateway := "gw-1-1"
				if scenario == "stopped" {
					status = "stopped"
					gateway = "gw-1-2"
				}
				if _, err = s.SQL().Exec("UPDATE instances SET status=? WHERE id=1", status); err != nil {
					t.Fatal(err)
				}
				limiter := &fakeGatewayRateLimitStore{}
				h := &AIGatewayHandler{service: actual, rateLimitStore: limiter}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Set("userID", 7)
				c.Set("instanceID", 1)
				c.Set("gatewayID", gateway)
				c.Set("gatewayKeyID", "key")
				body := `{"model":"auto","messages":[{"role":"user","content":"hello"}]}`
				if entry == "responses" {
					body = `{"model":"auto","input":"hello"}`
				}
				if entry == "anthropic" {
					body = `{"model":"auto","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`
				}
				if entry == "images" {
					body = `{"model":"image","prompt":"hello"}`
				}
				c.Request = httptest.NewRequest("POST", "/"+entry, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				switch entry {
				case "chat":
					h.ChatCompletions(c)
				case "responses":
					h.Responses(c)
				case "anthropic":
					h.AnthropicMessages(c)
				case "images":
					h.imageOperation(c, aigateway.ImageGenerate)
				}
				if rec.Code != 409 || limiter.input.RequestID != "" {
					t.Fatalf("fenced request reached policy/provider: status=%d body=%s limit=%+v", rec.Code, rec.Body.String(), limiter.input)
				}
				a, err := activity.GetActivity(t.Context(), 1)
				if err != nil || a.ActiveRequests != 0 || a.LastGatewayActivityAt != nil {
					t.Fatalf("rejected request changed activity: %+v %v", a, err)
				}
			})
		}
	}
}
