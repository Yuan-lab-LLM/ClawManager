package services

import (
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"strings"
)

//go:embed native_preview_bridge.js
var nativePreviewBridgeScript string

const nativePreviewHTMLLimit = 8 << 20

func nativePreviewBridgeDocument(r *http.Request, instanceType string) bool {
	_, managed := NormalizeV2RuntimeType(instanceType)
	if r.Method != http.MethodGet || !managed {
		return false
	}
	dest := r.Header.Get("Sec-Fetch-Dest")
	return dest == "iframe" || dest == "document" || (dest == "" && strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html"))
}

func nativeProxyHTMLCanRewrite(resp *http.Response) bool {
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity", "gzip":
		return true
	default:
		return false
	}
}

func readNativeProxyHTML(resp *http.Response, bounded bool) ([]byte, error) {
	var reader io.Reader = resp.Body
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		decoder, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer decoder.Close()
		reader = decoder
	default:
		return nil, fmt.Errorf("unsupported native HTML content encoding")
	}
	if bounded {
		reader = io.LimitReader(reader, nativePreviewHTMLLimit+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if bounded && len(body) > nativePreviewHTMLLimit {
		return nil, fmt.Errorf("native HTML exceeds bridge size limit")
	}
	resp.Header.Del("Content-Encoding")
	return body, nil
}

func injectNativePreviewBridge(html string) string {
	const marker = `id="clawmanager-native-preview-bridge"`
	if strings.Contains(html, marker) {
		return html
	}
	script := "<script " + marker + ">" + nativePreviewBridgeScript + "</script>"
	if head := openCodeBrowserHeadPattern.FindStringIndex(html); head != nil {
		return html[:head[1]] + script + html[head[1]:]
	}
	return script + html
}
