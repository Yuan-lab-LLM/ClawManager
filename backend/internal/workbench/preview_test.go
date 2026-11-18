package workbench

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParsePreviewURLAcceptsOnlyInstanceLoopback(t *testing.T) {
	for _, raw := range []string{"localhost:3000", "http://localhost:3000/login?q=1#form", "https://127.0.0.1:8443/", "http://[::1]:8000/", "http://0.0.0.0:5173/"} {
		t.Run(raw, func(t *testing.T) {
			target, err := ParsePreviewURL(raw)
			if err != nil || target.Port < 1 || target.Port > 65535 {
				t.Fatalf("valid local URL rejected: %+v %v", target, err)
			}
		})
	}
	for _, raw := range []string{"", "https://example.com/", "http://169.254.169.254/", "http://10.0.0.1:3000/", "http://localhost.evil.test:3000/", "http://user:secret@localhost:3000/", "file:///etc/passwd", "javascript:alert(1)", "http://localhost:0/", "http://localhost:65536/", "http://localhost:/", "http://localhost%2f.example.com/", "http://localhost:3000/\r\nHeader: bad", "http://[::1%25eth0]:3000/"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParsePreviewURL(raw); !errors.Is(err, ErrInvalidPreviewURL) {
				t.Fatalf("unsafe URL not rejected: %q: %v", raw, err)
			}
		})
	}
}

func TestPreviewOriginIsUniqueAndRetainsApplicationPath(t *testing.T) {
	store := &previewMemoryStore{values: map[string]PreviewLease{}}
	registry, err := NewPreviews("http://p-{token}.localhost:9001", store, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	scope := PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}
	one, err := registry.Create(context.Background(), scope, "http://localhost:3000/login%2Fcallback?q=one#form")
	if err != nil {
		t.Fatal(err)
	}
	two, err := registry.Create(context.Background(), scope, "http://localhost:3001/")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := url.Parse(registry.URL(one))
	b, _ := url.Parse(registry.URL(two))
	if a.Hostname() == b.Hostname() || a.Hostname() == "localhost" || a.EscapedPath() != "/login%2Fcallback" || a.RawQuery != "q=one" || a.Fragment != "form" {
		t.Fatalf("origins or route not isolated/preserved: %s %s", a, b)
	}
	resolved, err := registry.LookupHost(context.Background(), a.Host)
	if err != nil || resolved.Scope != scope || resolved.Target.Port != 3000 {
		t.Fatalf("wrong preview scope: %+v %v", resolved, err)
	}
	if _, err := registry.LookupHost(context.Background(), a.Hostname()+".evil.test"); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("foreign host accepted: %v", err)
	}
	for key, persisted := range store.values {
		if strings.Contains(key, one.ID) || persisted.ID != "" {
			t.Fatal("raw capability persisted in storage")
		}
	}
}

func TestPreviewLeasesWorkAcrossControlPlaneReplicasAndExpire(t *testing.T) {
	ctx := context.Background()
	store := &previewMemoryStore{values: map[string]PreviewLease{}}
	first, _ := NewPreviews("https://p-{token}.preview.example.test", store, time.Hour)
	second, _ := NewPreviews("https://p-{token}.preview.example.test", store, time.Hour)
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	first.now, second.now = func() time.Time { return now }, func() time.Time { return now }
	lease, err := first.Create(ctx, PreviewScope{InstanceID: 1, UserID: 2, Generation: 4, GatewayID: "gw-1-4"}, "localhost:3000")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(first.URL(lease))
	if _, err := second.LookupHost(ctx, u.Host); err != nil {
		t.Fatalf("another replica could not resolve lease: %v", err)
	}
	second.now = func() time.Time { return now.Add(time.Hour) }
	if _, err := second.LookupHost(ctx, u.Host); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("expired lease accepted: %v", err)
	}
}

func TestPreviewRegistryRejectsUnboundScopesAndNonIsolatedOrigins(t *testing.T) {
	store := &previewMemoryStore{values: map[string]PreviewLease{}}
	for _, template := range []string{"http://localhost:9001/preview/{token}", "http://{token}.localhost:9001", "https://p-{token}.example.test/?auth=1", "https://user:password@p-{token}.example.test"} {
		if _, err := NewPreviews(template, store, time.Hour); err == nil {
			t.Fatalf("unsafe origin template accepted: %s", template)
		}
	}
	registry, _ := NewPreviews("http://p-{token}.localhost:9001", store, time.Hour)
	if _, err := registry.Create(context.Background(), PreviewScope{InstanceID: 1, UserID: 2, Generation: 4, GatewayID: "gw-1-3"}, "localhost:3000"); !errors.Is(err, ErrPreviewScope) {
		t.Fatalf("stale gateway identity accepted: %v", err)
	}
}

type previewMemoryStore struct {
	mu     sync.Mutex
	values map[string]PreviewLease
}

func (s *previewMemoryStore) Put(_ context.Context, key string, lease PreviewLease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = lease
	return nil
}

func (s *previewMemoryStore) Get(_ context.Context, key string) (PreviewLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.values[key]
	if !ok {
		return PreviewLease{}, ErrPreviewNotFound
	}
	return lease, nil
}

func TestSharedPreviewLeaseNeverOutlivesShareSession(t *testing.T) {
	store := &previewMemoryStore{values: map[string]PreviewLease{}}
	registry, _ := NewPreviews("https://p-{token}.preview.test", store, time.Hour)
	now := time.Now()
	registry.now = func() time.Time { return now }
	shared := SharedPreviewAccess{Code: "sl_test", SessionBinding: strings.Repeat("a", 64), WorkspaceAccess: "read", ExpiresAt: now.Add(time.Minute)}
	scope := PreviewScope{InstanceID: 1, UserID: 2, Generation: 3, GatewayID: "gw-1-3"}
	lease, err := registry.CreateShared(context.Background(), scope, "localhost:4000", shared)
	if err != nil || lease.Shared == nil || !lease.ExpiresAt.Equal(shared.ExpiresAt) {
		t.Fatalf("share expiry not enforced: %+v %v", lease, err)
	}
	shared.WorkspaceAccess = "none"
	if _, err := registry.CreateShared(context.Background(), scope, "localhost:4000", shared); !errors.Is(err, ErrPreviewScope) {
		t.Fatalf("none permission accepted: %v", err)
	}
	registry.now = func() time.Time { return now.Add(time.Minute) }
	if _, err := registry.LookupCapability(context.Background(), lease.ID); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("expired share session accepted: %v", err)
	}
	shared.WorkspaceAccess = "read"
	if _, err := registry.CreateShared(context.Background(), scope, "localhost:4000", shared); !errors.Is(err, ErrPreviewScope) {
		t.Fatalf("expired share session minted preview: %v", err)
	}
}
