package browserorigin

import (
	"context"
	"errors"
	"testing"
)

type recordingStore struct {
	binding     Binding
	first, last int
	err         error
}

func (s *recordingStore) Bind(_ context.Context, id int, kind, target string, first, last int) (Binding, error) {
	s.first, s.last = first, last
	s.binding = Binding{Port: first, InstanceID: id, Kind: kind, Target: target}
	return s.binding, s.err
}
func (s *recordingStore) ByPort(_ context.Context, port int) (Binding, error) {
	if s.err != nil {
		return Binding{}, s.err
	}
	if port != s.binding.Port {
		return Binding{}, ErrNotFound
	}
	return s.binding, nil
}

func TestRegistryOriginAndRangeValidation(t *testing.T) {
	for _, tc := range []struct{ origin, ports string }{
		{"http://manager.example:8443", "31000-31001"},
		{"https://user@manager.example:8443", "31000-31001"},
		{"https://manager.example:8443/path", "31000-31001"},
		{"https://manager.example:8443?", "31000-31001"},
		{"https://manager.example:8443#fragment", "31000-31001"},
		{"https://*.example:8443", "31000-31001"},
		{"https://manager.example:", "31000-31001"},
		{"https://manager.example:0", "31000-31001"},
		{"https://manager.example:8443", ""},
		{"https://manager.example:8443", "0-12"},
		{"https://manager.example:8443", "31001-31000"},
		{"https://manager.example:8443", "31000-31512"},
		{"https://manager.example:8443", "65535-65536"},
		{"https://manager.example:8443", "8443-8444"},
		{"https://manager.example", "443-444"},
	} {
		t.Run(tc.origin+"/"+tc.ports, func(t *testing.T) {
			if _, err := New(tc.origin, tc.ports, &recordingStore{}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted invalid config: %v", err)
			}
		})
	}
	if _, err := New("https://manager.example", "31000-31001", nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	r, err := New("https://Manager.Example:8443", "31000-31511", &recordingStore{})
	if err != nil {
		t.Fatal(err)
	}
	if r.ManagerOrigin() != "https://manager.example:8443" {
		t.Fatal(r.ManagerOrigin())
	}
	if a, b := r.Range(); a != 31000 || b != 31511 {
		t.Fatalf("range %d-%d", a, b)
	}
}

func TestRegistryExactHostAndPortBoundary(t *testing.T) {
	s := &recordingStore{}
	r, err := New("https://manager.example:8443", "31000-31001", s)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Bind(context.Background(), 42, Native, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	if s.first != 31000 || s.last != 31001 || r.URL(b) != "https://manager.example:31000" {
		t.Fatalf("bad binding %+v", b)
	}
	got, err := r.Lookup(context.Background(), "MANAGER.EXAMPLE:31000")
	if err != nil || got != b {
		t.Fatalf("lookup %+v %v", got, err)
	}
	for _, host := range []string{"manager.example", "manager.example:8443", "manager.example:31002", "evil.example:31000", "manager.example.evil:31000", "manager.example:031000", "manager.example:31000/path", "manager.example:31000@evil", "manager.example.:31000", "https://manager.example:31000"} {
		if r.Matches(host) {
			t.Errorf("matched %q", host)
		}
		if _, err := r.Lookup(context.Background(), host); !errors.Is(err, ErrNotFound) {
			t.Errorf("lookup %q: %v", host, err)
		}
	}
	if !r.Matches("manager.example:31001") {
		t.Fatal("unassigned pool port should match routing boundary")
	}
	if _, err := r.Lookup(context.Background(), "manager.example:31001"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s.err = ErrCapacity
	if _, err := r.Bind(context.Background(), 42, Native, "opencode"); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestRegistryTargetValidationAndCanonicalization(t *testing.T) {
	s := &recordingStore{}
	r, _ := New("https://manager.example:8443", "31000-31099", s)
	for _, tc := range []struct{ kind, input, want string }{
		{Native, "opencode", "opencode"}, {Native, "deepseek-harness", "deepseek-harness"},
		{Preview, "localhost:3000", "http://localhost:3000/"},
		{Preview, "http://0.0.0.0:3000/", "http://127.0.0.1:3000/"},
		{Preview, "https://[::1]", "https://[::1]:443/"},
		{Preview, "HTTP://LOCALHOST:80/", "http://localhost:80/"},
	} {
		b, err := r.Bind(context.Background(), 42, tc.kind, tc.input)
		if err != nil || b.Target != tc.want {
			t.Fatalf("%q: %+v %v", tc.input, b, err)
		}
	}
	for _, tc := range []struct{ kind, target string }{
		{Native, "hermes"}, {Native, "http://localhost:3000"}, {"other", "opencode"},
		{Preview, "http://example.com:3000"}, {Preview, "http://169.254.169.254"},
		{Preview, "http://localhost:3000/path"}, {Preview, "http://localhost:3000/?"},
		{Preview, "http://localhost:3000/#"}, {Preview, "http://user@localhost:3000"},
		{Preview, "http://localhost:3000/%2f"}, {Preview, "http://localhost:0"},
		{Preview, "http://localhost:3000/\n"},
	} {
		if _, err := r.Bind(context.Background(), 42, tc.kind, tc.target); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %s %q: %v", tc.kind, tc.target, err)
		}
	}
	if _, err := r.Bind(context.Background(), 0, Native, "opencode"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestRegistryIPOriginsAndCancellation(t *testing.T) {
	for _, tc := range []struct{ origin, host string }{
		{"https://10.130.81.54:30443", "10.130.81.54:31000"},
		{"https://[::1]:30443", "[::1]:31000"},
	} {
		r, err := New(tc.origin, "31000-31001", &recordingStore{})
		if err != nil || r.ManagerOrigin() != tc.origin || !r.Matches(tc.host) {
			t.Fatalf("IP origin: %+v %v", r, err)
		}
		b, err := r.Bind(context.Background(), 42, Native, "opencode")
		if err != nil || r.URL(b) != "https://"+tc.host {
			t.Fatalf("IP binding %+v %v", b, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := r.Bind(ctx, 42, Native, "opencode"); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := r.Lookup(ctx, tc.host); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if r.URL(Binding{Port: 31002}) != "" {
			t.Fatal("URL escaped pool")
		}
	}
}
