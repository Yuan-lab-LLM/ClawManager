package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestHermesDashboardHTMLAssetsUseInstancePrefixWithoutEditingContent(t *testing.T) {
	const prefix = "/api/v1/instances/214/proxy"
	const body = `<!doctype html><html><head data-test="yes">
<script type="module" crossorigin src="/assets/index-DP3DHjhB.js"></script>
<link rel="modulepreload" crossorigin href="/assets/vendor-B7eZBWFZ.js">
<link rel="stylesheet" href='/assets/index-mXYHKWVT.css'>
<link rel="icon" href="/favicon.ico">
<link rel="stylesheet" href="//cdn.example/assets/external.css">
<script>window.example="/assets/user-content.js";</script>
<style>p{content:"/assets/literal.css";background:url(/assets/a.png)}</style>
</head><body><a href="/api/status">API link</a><p>/assets/user-content.js</p>
<img src="/ds-assets/icon.svg"><p style="background: url('/assets/a.png')">literal</p>
</body></html>`
	got := rewriteHermesDashboardHTMLAssets(body, prefix)
	for _, expected := range []string{
		`src="` + prefix + `/assets/index-DP3DHjhB.js"`,
		`href="` + prefix + `/assets/vendor-B7eZBWFZ.js"`,
		`href="` + prefix + `/assets/index-mXYHKWVT.css"`,
		`href="` + prefix + `/favicon.ico"`,
		`src="` + prefix + `/ds-assets/icon.svg"`,
		`background:url(` + prefix + `/assets/a.png)`,
		`window.example="/assets/user-content.js"`,
		`content:"/assets/literal.css"`,
		`<p>/assets/user-content.js</p>`,
		`href="//cdn.example/assets/external.css"`,
		`<a href="/api/status">`,
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("rewriting lost %q", expected)
		}
	}
	bridge := strings.Index(got, `<script id="cm-hermes-assets">`)
	if bridge < 0 || bridge > strings.Index(got, `<script type="module"`) || bridge > strings.Index(got, `rel="modulepreload"`) {
		t.Fatal("asset bridge must precede every native module/preload request")
	}
	if again := rewriteHermesDashboardHTMLAssets(got, prefix); again != got {
		t.Fatal("asset adaptation is not idempotent")
	}
	if got := rewriteHermesDashboardHTMLAssets(body, `//foreign.example`); got != body {
		t.Fatal("invalid asset destination accepted")
	}
}

func TestHermesDashboardCSSRewritesOnlyResourceTokens(t *testing.T) {
	const prefix = "/api/v1/instances/214/proxy"
	for _, tc := range []struct{ input, expected string }{
		{`a{src:url(/fonts-terminal/JetBrainsMono-Regular.woff2)}`, `a{src:url(` + prefix + `/fonts-terminal/JetBrainsMono-Regular.woff2)}`},
		{`a{src:URL( '/fonts/a.woff2' )}`, `a{src:URL( '` + prefix + `/fonts/a.woff2' )}`},
		{`@import "/assets/theme.css" screen;`, `@import "` + prefix + `/assets/theme.css" screen;`},
		{`@import url("/assets/theme.css");`, `@import url("` + prefix + `/assets/theme.css");`},
		{`p{background:url(/ds-assets/p.png#sprite)}`, `p{background:url(` + prefix + `/ds-assets/p.png#sprite)}`},
		{`p{content:"url(/assets/not-a-resource.png)"}`, `p{content:"url(/assets/not-a-resource.png)"}`},
		{`/* url(/assets/not-a-resource.png) */`, `/* url(/assets/not-a-resource.png) */`},
		{`p{background:url(data:image/svg+xml;base64,abc)}`, `p{background:url(data:image/svg+xml;base64,abc)}`},
		{`p{background:url(https://cdn.example/assets/a.png)}`, `p{background:url(https://cdn.example/assets/a.png)}`},
		{`p{background:url(//cdn.example/assets/a.png)}`, `p{background:url(//cdn.example/assets/a.png)}`},
		{`p{background:url(/api/v1/instances/215/proxy/assets/a.png)}`, `p{background:url(/api/v1/instances/215/proxy/assets/a.png)}`},
		{`p{background:url(/api/config)}`, `p{background:url(/api/config)}`},
		{`p{background:url(/assets/../secret.png)}`, `p{background:url(/assets/../secret.png)}`},
		{`p{background:url(/assets/escape\d.png)}`, `p{background:url(/assets/escape\d.png)}`},
		{`p{--x:myurl(/assets/a.png)}`, `p{--x:myurl(/assets/a.png)}`},
		{`p{content:"unterminated`, `p{content:"unterminated`},
	} {
		if got := rewriteHermesDashboardCSSAssets(tc.input, prefix); got != tc.expected {
			t.Errorf("input=%q got=%q expected=%q", tc.input, got, tc.expected)
		}
	}
}

func TestHermesDashboardProductionBFFAdaptsAssetsButPreservesModules(t *testing.T) {
	// Actual deployed Vite 8 output uses relative native imports; its preload
	// dependency arrays use root-resolved "assets/..." names. Keep code intact.
	const module = "import{a}from\"./vendor-B7eZBWFZ.js\";export const open=()=>import(`./ChatPage-oNWcZI--.js`);const text='/assets/user-content.js';"
	const css = `@font-face{src:url(/fonts-terminal/JetBrainsMono-Regular.woff2)}p{content:"/assets/user-content.css"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/password-login":
			dashboardTestLogin(t, w, r)
		case "/chat":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<html><head><script>window.__HERMES_AUTH_REQUIRED__=true;window.__HERMES_BASE_PATH__="/api/v1/instances/123/proxy";</script><script type="module" src="/assets/index-DP3DHjhB.js"></script><link rel="modulepreload" href="/assets/vendor-B7eZBWFZ.js"></head><body>Chat shell</body></html>`)
		case "/assets/index-DP3DHjhB.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = io.WriteString(w, module)
		case "/assets/index-mXYHKWVT.css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = io.WriteString(w, css)
		case "/fonts-terminal/JetBrainsMono-Regular.woff2":
			w.Header().Set("Content-Type", "font/woff2")
			_, _ = io.WriteString(w, "font-data")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := dashboardServiceFixture(t, upstream.URL)
	claims, _ := dashboardActivateClaims(t, s)
	request := func(path string) string {
		t.Helper()
		w := httptest.NewRecorder()
		if err := s.ProxyDashboardHTTP(context.Background(), claims, http.MethodGet, path, nil, w); err != nil {
			t.Fatalf("production BFF %s failed: %v", path, err)
		}
		return w.Body.String()
	}
	page := request("/chat")
	tokens := html.NewTokenizer(strings.NewReader(page))
	for kind := tokens.Next(); kind != html.ErrorToken; kind = tokens.Next() {
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		for _, attr := range tokens.Token().Attr {
			if (attr.Key == "href" || attr.Key == "src") && strings.HasPrefix(attr.Val, "/assets/") {
				t.Fatal("native parser asset request escaped the instance proxy")
			}
		}
	}
	if got := request("/assets/index-DP3DHjhB.js"); got != module {
		t.Fatal("JavaScript content/native module specifiers were modified")
	}
	if got := request("/assets/index-mXYHKWVT.css"); !strings.Contains(got, "url(/api/v1/instances/123/proxy/fonts-terminal/") || !strings.Contains(got, `content:"/assets/user-content.css"`) {
		t.Fatal("CSS asset/content boundary failed")
	}
	if got := request("/fonts-terminal/JetBrainsMono-Regular.woff2"); got != "font-data" {
		t.Fatal("terminal font did not pass the authenticated asset route")
	}
	for _, path := range []string{"/fonts-terminal/../private.woff2", "/fonts-terminal/config.yaml", "/fonts-terminal/a.woff2/extra", "/fonts-terminal//a.woff2"} {
		if _, err := hermesDashboardHTTPPolicy(http.MethodGet, path, nil); err == nil {
			t.Errorf("terminal font allowlist accepted %q", path)
		}
	}
}
