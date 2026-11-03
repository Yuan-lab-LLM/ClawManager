package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type openCodeBootstrapRuntime struct {
	mu                 sync.Mutex
	sessions           map[string]openCodeInitialSession
	creates            int
	lostCreateResponse bool
	createStatus       int
	infoStatus         int
	infoHTML           bool
	version            string
	pages              map[string][]openCodeInitialSession
}

func (runtime *openCodeBootstrapRuntime) serve(w http.ResponseWriter, r *http.Request) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	username, password, ok := r.BasicAuth()
	if !ok || username != "opencode" || password != "test-bootstrap-password" {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	write := func(value any) { _ = json.NewEncoder(w).Encode(value) }
	if r.URL.Path == "/api/info" {
		if runtime.infoStatus != 0 {
			w.WriteHeader(runtime.infoStatus)
			return
		}
		if runtime.infoHTML {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html>v1 fallback</html>`))
			return
		}
		version := runtime.version
		if version == "" {
			version = "2.0.27"
		}
		write(map[string]any{"version": version})
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/session" {
		var request struct {
			ID       string `json:"id"`
			Location struct {
				Directory string `json:"directory"`
			} `json:"location"`
			Metadata map[string]string `json:"metadata"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Metadata["clawmanager.bootstrap"] == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if runtime.createStatus != 0 {
			w.WriteHeader(runtime.createStatus)
			return
		}
		if _, exists := runtime.sessions[request.ID]; exists {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if runtime.sessions == nil {
			runtime.sessions = make(map[string]openCodeInitialSession)
		}
		session := openCodeInitialSession{ID: request.ID}
		session.Location.Directory = request.Location.Directory
		runtime.sessions[request.ID] = session
		runtime.creates++
		if runtime.lostCreateResponse {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		write(map[string]any{"data": session})
		return
	}
	if r.URL.Path == "/api/session" {
		if runtime.pages != nil {
			cursor := r.URL.Query().Get("cursor")
			var next *string
			if cursor == "" {
				value := "second-page"
				next = &value
			}
			write(map[string]any{"data": runtime.pages[cursor], "cursor": map[string]any{"next": next}})
			return
		}
		items := make([]openCodeInitialSession, 0, len(runtime.sessions))
		for _, session := range runtime.sessions {
			items = append(items, session)
		}
		write(map[string]any{"data": items})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/session/") {
		session, exists := runtime.sessions[strings.TrimPrefix(r.URL.Path, "/api/session/")]
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		write(map[string]any{"data": session})
		return
	}
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(`<html><head></head><body>native page</body></html>`))
}

func newOpenCodeBootstrapFixture(t *testing.T, runtime *openCodeBootstrapRuntime, instanceID int) (*InstanceProxyService, string, *url.URL) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(runtime.serve))
	t.Cleanup(upstream.Close)
	service, token := newOpenCodeV2ProxyTestService(t, upstream.URL, instanceID, "test-bootstrap-password")
	service.httpClient = upstream.Client()
	WithOpenCodeLiteSessionBootstrap()(service)
	parsed, _ := url.Parse(upstream.URL)
	return service, token.Token, parsed
}

func TestOpenCodeBootstrapSurvivesRefreshAndControlPlaneRestart(t *testing.T) {
	runtime := &openCodeBootstrapRuntime{}
	service, token, _ := newOpenCodeBootstrapFixture(t, runtime, 701)
	var selected string
	for iteration := 0; iteration < 3; iteration++ {
		request := httptest.NewRequest(http.MethodGet, "https://opencode-701.local:30443/api/v1/instances/701/proxy/", nil)
		request.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
		recorder := httptest.NewRecorder()
		if err := service.ProxyRequest(context.Background(), 701, token, recorder, request); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusTemporaryRedirect || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid initial navigation: %d", recorder.Code)
		}
		route := recorder.Header().Get("Location")
		parts := strings.Split(route, "/")
		origin, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil || string(origin) != "https://opencode-701.local:30443" {
			t.Fatalf("wrong native server route: %s", route)
		}
		if selected != "" && selected != parts[4] {
			t.Fatal("refresh created a different session")
		}
		selected = parts[4]
		if strings.Contains(route, "test-bootstrap-password") || strings.Contains(route, token) {
			t.Fatal("credential exposed in native route")
		}
		if iteration == 1 {
			// A distinct service represents a restart with no in-memory bootstrap state.
			service, token, _ = newOpenCodeBootstrapFixture(t, runtime, 701)
		}
	}
	if runtime.creates != 1 || len(runtime.sessions) != 1 {
		t.Fatalf("starter duplicated: %d creates, %d sessions", runtime.creates, len(runtime.sessions))
	}
	if runtime.sessions[selected].Location.Directory != "/workspaces/opencode/user-45/instance-701/project" {
		t.Fatal("starter is outside the default project")
	}
}

func TestOpenCodeBootstrapSeparatesInstancesInSharedRuntime(t *testing.T) {
	runtime := &openCodeBootstrapRuntime{}
	first, _, firstUpstream := newOpenCodeBootstrapFixture(t, runtime, 707)
	second, _, secondUpstream := newOpenCodeBootstrapFixture(t, runtime, 708)
	firstID, err := first.ensureOpenCodeInitialSession(context.Background(), 707, firstUpstream, "test-bootstrap-password")
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := second.ensureOpenCodeInitialSession(context.Background(), 708, secondUpstream, "test-bootstrap-password")
	if err != nil {
		t.Fatal(err)
	}
	if firstID == secondID || runtime.creates != 2 {
		t.Fatal("a shared runtime reused another instance's starter")
	}
	if !strings.Contains(runtime.sessions[firstID].Location.Directory, "instance-707/") || !strings.Contains(runtime.sessions[secondID].Location.Directory, "instance-708/") {
		t.Fatal("starter escaped the instance workspace")
	}
}

func TestOpenCodeBootstrapDoesNotReopenArchivedStarter(t *testing.T) {
	runtime := &openCodeBootstrapRuntime{}
	service, _, upstream := newOpenCodeBootstrapFixture(t, runtime, 709)
	firstID, err := service.ensureOpenCodeInitialSession(context.Background(), 709, upstream, "test-bootstrap-password")
	if err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	archived := runtime.sessions[firstID]
	archived.Time.Archived = 1
	runtime.sessions[firstID] = archived
	runtime.mu.Unlock()
	id, err := service.ensureOpenCodeInitialSession(context.Background(), 709, upstream, "test-bootstrap-password")
	if err != nil || id != "" || runtime.creates != 1 {
		t.Fatalf("archiving the starter was undone: %s %v", id, err)
	}
}

func TestOpenCodeBootstrapConcurrentReplicasShareOneSession(t *testing.T) {
	runtime := &openCodeBootstrapRuntime{}
	one, _, upstreamOne := newOpenCodeBootstrapFixture(t, runtime, 702)
	two, _, upstreamTwo := newOpenCodeBootstrapFixture(t, runtime, 702)
	start := make(chan struct{})
	results := make(chan string, 12)
	errors := make(chan error, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			service, upstream := one, upstreamOne
			if index%2 == 1 {
				service, upstream = two, upstreamTwo
			}
			id, err := service.ensureOpenCodeInitialSession(context.Background(), 702, upstream, "test-bootstrap-password")
			if err != nil {
				errors <- err
				return
			}
			results <- id
		}(i)
	}
	close(start)
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	var expected string
	for id := range results {
		if expected != "" && id != expected {
			t.Fatal("replicas selected different starters")
		}
		expected = id
	}
	if expected == "" || runtime.creates != 1 || len(runtime.sessions) != 1 {
		t.Fatalf("concurrent bootstrap duplicated session: %d", runtime.creates)
	}
}

func TestOpenCodeBootstrapRecoversLostCreateResponse(t *testing.T) {
	runtime := &openCodeBootstrapRuntime{lostCreateResponse: true}
	service, _, upstream := newOpenCodeBootstrapFixture(t, runtime, 703)
	id, err := service.ensureOpenCodeInitialSession(context.Background(), 703, upstream, "test-bootstrap-password")
	if err != nil || id == "" || runtime.creates != 1 {
		t.Fatalf("could not recover committed session: %s %v", id, err)
	}
}

func TestOpenCodeBootstrapAdoptsExistingSessionAcrossPages(t *testing.T) {
	existing := openCodeInitialSession{ID: "ses_existing_conversation"}
	existing.Location.Directory = "/workspaces/opencode/user-45/instance-704/uploaded-project"
	foreign := openCodeInitialSession{ID: "ses_foreign_conversation"}
	foreign.Location.Directory = "/workspaces/opencode/user-45/instance-705/project"
	runtime := &openCodeBootstrapRuntime{pages: map[string][]openCodeInitialSession{"": {foreign}, "second-page": {existing}}}
	service, _, upstream := newOpenCodeBootstrapFixture(t, runtime, 704)
	id, err := service.ensureOpenCodeInitialSession(context.Background(), 704, upstream, "test-bootstrap-password")
	if err != nil || id != existing.ID || runtime.creates != 0 {
		t.Fatalf("existing session was not adopted: %s %v", id, err)
	}
}

func TestOpenCodeBootstrapRejectsCreationFailure(t *testing.T) {
	runtime := &openCodeBootstrapRuntime{createStatus: http.StatusBadRequest}
	service, _, upstream := newOpenCodeBootstrapFixture(t, runtime, 705)
	if _, err := service.ensureOpenCodeInitialSession(context.Background(), 705, upstream, "test-bootstrap-password"); err == nil {
		t.Fatal("creation failure was hidden")
	}
}

func TestOpenCodeBootstrapLeavesV1AndDirectSessionNavigationAlone(t *testing.T) {
	for _, kind := range []string{"404", "html", "v1-version"} {
		t.Run(kind, func(t *testing.T) {
			runtime := &openCodeBootstrapRuntime{}
			switch kind {
			case "404":
				runtime.infoStatus = 404
			case "html":
				runtime.infoHTML = true
			case "v1-version":
				runtime.version = "1.18.13"
			}
			service, token, _ := newOpenCodeBootstrapFixture(t, runtime, 706)
			for _, suffix := range []string{"/", "/server/local/session/ses_user_selected"} {
				request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/instances/706/proxy%s", suffix), nil)
				request.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
				request.Header.Set("Accept", "text/html")
				recorder := httptest.NewRecorder()
				if err := service.ProxyRequest(context.Background(), 706, token, recorder, request); err != nil {
					t.Fatal(err)
				}
				if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "clawmanager-opencode-storage-scope") {
					t.Fatal("native navigation was replaced")
				}
			}
			if runtime.creates != 0 {
				t.Fatal("v1 or direct navigation triggered a starter")
			}
		})
	}
}
