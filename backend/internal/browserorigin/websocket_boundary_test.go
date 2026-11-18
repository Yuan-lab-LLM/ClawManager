package browserorigin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeWebSocketWithoutFetchMetadata(t *testing.T) {
	const native = "https://cm.test:31400"
	for _, tc := range []struct {
		name, method, origin, site, connection, upgrade string
		want                                            bool
	}{
		{"Chrome native socket", "GET", native, "", "Upgrade", "websocket", true},
		{"connection token list", "GET", native, "", "keep-alive, Upgrade", "WebSocket", true},
		{"modern native socket", "GET", native, "same-origin", "Upgrade", "websocket", true},
		{"other native port", "GET", "https://cm.test:31401", "", "Upgrade", "websocket", false},
		{"manager port", "GET", "https://cm.test:30443", "", "Upgrade", "websocket", false},
		{"foreign site", "GET", "https://evil.test", "", "Upgrade", "websocket", false},
		{"null origin", "GET", "null", "", "Upgrade", "websocket", false},
		{"missing origin", "GET", "", "", "Upgrade", "websocket", false},
		{"contradictory same-site metadata", "GET", native, "same-site", "Upgrade", "websocket", false},
		{"contradictory cross-site metadata", "GET", native, "cross-site", "Upgrade", "websocket", false},
		{"ordinary HTTP", "GET", native, "", "", "", false},
		{"POST upgrade", "POST", native, "", "Upgrade", "websocket", false},
		{"missing connection upgrade", "GET", native, "", "keep-alive", "websocket", false},
		{"wrong upgrade protocol", "GET", native, "", "Upgrade", "h2c", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, native+"/api/remote.mux", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("Connection", tc.connection)
			r.Header.Set("Upgrade", tc.upgrade)
			if got := AllowNativeRequest(r, native, "https://cm.test:30443", false); got != tc.want {
				t.Fatalf("allowed=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestManagerWebSocketDoesNotUseNativeFallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "https://cm.test:30443/api/v1/ws", nil)
	r.Header.Set("Origin", "https://cm.test:31400")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	if AllowManagerRequest(r, "https://cm.test:30443", "cm.test", 31400, 31527) {
		t.Fatal("native socket crossed into the manager origin")
	}
}
