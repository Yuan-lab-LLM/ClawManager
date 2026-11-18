package k8s

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeTemplateHelperFailuresRemainUnproven(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		valid        bool
	}{
		{"missing", "", false}, {"timeout", "#!/bin/sh\nexec sleep 5\n", false}, {"nonzero", "#!/bin/sh\nexit 1\n", false},
		{"bad-output", "#!/bin/sh\nprintf invalid\n", false}, {"wrong-version", "#!/bin/sh\nprintf '%s' '{\"api_version\":\"v1.32.3\",\"hash\":\"abc\"}'\n", false},
		{"excess-output", "#!/bin/sh\nexec yes x\n", false},
		{"trailing", "#!/bin/sh\nprintf '%s' '{\"api_version\":\"v1.35.0\",\"hash\":\"abc\"} {}'\n", false},
		{"valid", "#!/bin/sh\nprintf '%s' '{\"api_version\":\"v1.35.0\",\"hash\":\"abc\"}'\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "helper")
			if tc.script != "" {
				if err := os.WriteFile(binary, []byte(tc.script), 0755); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err := runRuntimeTemplateHashHelper(ctx, binary, []byte(`{}`))
			if (err == nil) != tc.valid {
				t.Fatalf("%s: %v", tc.name, err)
			}
		})
	}
}

func TestRuntimeTemplateRejectsMixedReplicaSetResponse(t *testing.T) {
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: "uid", ResourceVersion: "17"}}
	for _, raw := range []string{`{"metadata":{"name":"pool","namespace":"runtime","uid":"different","resourceVersion":"17"},"spec":{"template":{}}}`, `{"metadata":{"name":"pool","namespace":"runtime","uid":"uid","resourceVersion":"18"},"spec":{"template":{}}}`} {
		if _, err := matchingRawRuntimeTemplate([]byte(raw), rs); err == nil {
			t.Fatal("mixed source response accepted")
		}
	}
	if _, err := matchingRawRuntimeTemplate([]byte(`{"metadata":{"name":"pool","namespace":"runtime","uid":"uid","resourceVersion":"17"},"spec":{"template":{}}}`), rs); err != nil {
		t.Fatal(err)
	}
}
