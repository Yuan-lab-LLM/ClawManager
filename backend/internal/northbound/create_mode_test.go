package northbound

import "testing"

func TestCreateModeMatchesEndpoint(t *testing.T) {
	for _, endpoint := range []string{"lite", "pro"} {
		for _, mode := range []string{"", "lite", "pro", "unknown", " LITE "} {
			want := mode == "" || mode == endpoint || (mode == " LITE " && endpoint == "lite")
			if got := validateNorthboundCreateMode(mode, endpoint) == nil; got != want {
				t.Fatalf("endpoint=%s mode=%q accepted=%v", endpoint, mode, got)
			}
		}
	}
}
