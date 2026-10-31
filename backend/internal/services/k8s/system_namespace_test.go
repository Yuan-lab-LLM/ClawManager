package k8s

import "testing"

func TestClientSystemNamespaceAcceptsPrefixOrFullNamespace(t *testing.T) {
	for _, tc := range []struct{ configured, want string }{
		{"clawreef", "clawreef-system"},
		{"clawmanager", "clawmanager-system"},
		{"clawmanager-system", "clawmanager-system"},
		{"tenant-system", "tenant-system"},
		{" ClawManager-System ", "clawmanager-system"},
		{"", "system"},
	} {
		t.Run(tc.configured, func(t *testing.T) {
			client := &Client{Namespace: tc.configured}
			if got := client.GetSystemNamespace(); got != tc.want {
				t.Fatalf("GetSystemNamespace(%q) = %q, want %q", tc.configured, got, tc.want)
			}
		})
	}
}
