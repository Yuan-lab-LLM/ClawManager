package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// originalInstanceProxyRequest returns the private request captured before log
// redaction. Downstream proxy code needs the credentials, while Gin log and
// recovery middleware must only see the sanitized public request.
func originalInstanceProxyRequest(c *gin.Context) *http.Request {
	request := c.Request.Clone(c.Request.Context())
	if headers, exists := c.Get("instanceProxyOriginalHeaders"); exists {
		request.Header = headers.(http.Header).Clone()
	}
	if query, exists := c.Get("instanceProxyOriginalQuery"); exists {
		request.URL.RawQuery = query.(string)
	}
	return request
}

func redactInstanceProxyCredentials(c *gin.Context) {
	if !strings.HasPrefix(c.Request.URL.Path, "/api/v1/instances/") {
		return
	}
	if strings.HasSuffix(c.Request.URL.Path, "/access") {
		c.Request.URL.RawQuery = ""
		c.Request.RequestURI = c.Request.URL.RequestURI()
		c.Request.Header.Del("Cookie")
		c.Request.Header.Del("Referer")
		c.Request.Header.Del("Sec-WebSocket-Protocol")
		return
	}
	if !strings.Contains(c.Request.URL.Path, "/proxy") {
		return
	}
	c.Set("instanceProxyOriginalHeaders", c.Request.Header.Clone())
	c.Set("instanceProxyOriginalQuery", c.Request.URL.RawQuery)
	query := c.Request.URL.Query()
	for key := range query {
		lower := strings.ToLower(strings.ReplaceAll(key, "_", ""))
		if strings.Contains(lower, "token") || strings.Contains(lower, "ticket") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") || strings.Contains(lower, "apikey") {
			query.Del(key)
		}
	}
	c.Request.URL.RawQuery = query.Encode()
	c.Request.RequestURI = c.Request.URL.RequestURI()
	for key := range c.Request.Header {
		lower := strings.ToLower(key)
		if lower == "cookie" || lower == "referer" || lower == "sec-websocket-protocol" || strings.Contains(lower, "authorization") || strings.Contains(lower, "token") || strings.Contains(lower, "api-key") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
			c.Request.Header.Del(key)
		}
	}
}
