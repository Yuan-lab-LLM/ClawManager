package handlers

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// EgressProxyHandler provides a minimal forward proxy for ordinary HTTP/HTTPS traffic.
type EgressProxyHandler struct {
	transport *http.Transport
}

// NewEgressProxyHandler creates a new egress proxy handler.
func NewEgressProxyHandler() *EgressProxyHandler {
	return &EgressProxyHandler{
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// Handle proxies ordinary HTTP or HTTPS CONNECT traffic.
func (h *EgressProxyHandler) Handle(c *gin.Context) {
	if strings.EqualFold(c.Request.Method, http.MethodConnect) {
		h.handleConnect(c)
		return
	}

	if c.Request.URL == nil || c.Request.URL.Scheme == "" || c.Request.URL.Host == "" {
		c.Status(http.StatusNotFound)
		return
	}

	outReq := c.Request.Clone(c.Request.Context())
	outReq.RequestURI = ""
	removeHopHeaders(outReq.Header)

	resp, err := h.transport.RoundTrip(outReq)
	if err != nil {
		c.String(http.StatusBadGateway, "proxy upstream error: %v", err)
		return
	}
	defer resp.Body.Close()

	removeHopHeaders(resp.Header)
	copyHeaders(c.Writer.Header(), resp.Header)
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}

func (h *EgressProxyHandler) handleConnect(c *gin.Context) {
	target := strings.TrimSpace(c.Request.Host)
	if target == "" {
		c.String(http.StatusBadRequest, "missing CONNECT target")
		return
	}

	upstreamConn, err := net.DialTimeout("tcp", target, 30*time.Second)
	if err != nil {
		c.String(http.StatusBadGateway, "proxy connect error: %v", err)
		return
	}

	hijacker, ok := c.Writer.(http.Hijacker)
	if !ok {
		_ = upstreamConn.Close()
		c.String(http.StatusInternalServerError, "proxy hijacking not supported")
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		_ = upstreamConn.Close()
		return
	}

	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	go tunnelConns(upstreamConn, clientConn)
	go tunnelConns(clientConn, upstreamConn)
}

func tunnelConns(dst net.Conn, src net.Conn) {
	defer dst.Close()
	defer src.Close()
	_, _ = io.Copy(dst, src)
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func removeHopHeaders(headers http.Header) {
	for _, key := range []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		headers.Del(key)
	}
}
