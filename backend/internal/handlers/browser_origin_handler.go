package handlers

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"clawreef/internal/browserorigin"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

// BrowserOriginRoutes runs before CM CORS and never exposes manager routes on an
// assigned application port. Authentication stays in the instance proxy handler.
func BrowserOriginRoutes(reg *browserorigin.Registry, instances *InstanceHandler, wb *services.InstanceWorkbenchService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if reg == nil {
			if c.GetHeader("X-ClawManager-Browser-Port") == "1" {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			c.Next()
			return
		}
		manager, _ := url.Parse(reg.ManagerOrigin())
		if !reg.Matches(c.Request.Host) {
			if c.GetHeader("X-ClawManager-Browser-Port") == "1" {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			if strings.EqualFold(c.Request.Host, manager.Host) && c.Request.URL.Path == "/__clawmanager_browser/view" {
				if wb == nil {
					c.AbortWithStatus(http.StatusServiceUnavailable)
					return
				}
				wb.ServeBrowserViewer(c.Writer, c.Request)
				c.Abort()
				return
			}
			if _, rawPort, err := net.SplitHostPort(c.Request.Host); err == nil {
				port, _ := strconv.Atoi(rawPort)
				first, last := reg.Range()
				if port >= first && port <= last {
					c.AbortWithStatus(http.StatusNotFound)
					return
				}
			}
			if strings.EqualFold(c.Request.Host, manager.Host) && strings.HasPrefix(c.Request.URL.Path, "/api/") {
				first, last := reg.Range()
				if !browserorigin.AllowManagerRequest(c.Request, reg.ManagerOrigin(), manager.Hostname(), first, last) {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
			}
			c.Next()
			return
		}
		binding, err := reg.Lookup(c.Request.Context(), c.Request.Host)
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if c.Request.URL.Path == "/__clawmanager_cert_check" || c.Request.URL.Path == "/__clawmanager_cert_trust" {
			c.Header("Cache-Control", "no-store")
			c.Header("Cross-Origin-Resource-Policy", "cross-origin")
			if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
				c.Header("Allow", "GET, HEAD")
				c.AbortWithStatus(http.StatusMethodNotAllowed)
				return
			}
			if c.Request.URL.Path == "/__clawmanager_cert_check" {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
			c.Header("X-Frame-Options", "DENY")
			c.Status(http.StatusOK)
			if c.Request.Method == http.MethodGet {
				_, _ = fmt.Fprint(c.Writer, `<!doctype html><html><head><meta charset="utf-8"><title>Certificate confirmed</title></head><body><h1>证书确认完成 / Certificate confirmed</h1><p>请关闭此标签页并返回 ClawManager。</p><p>Close this tab and return to ClawManager.</p></body></html>`)
			}
			c.Abort()
			return
		}

		if binding.Kind == browserorigin.Preview {
			if wb == nil {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			privateRequest := c.Request.Clone(c.Request.Context())
			c.Request.URL.Path = "/__clawmanager_browser/request"
			c.Request.URL.RawPath = ""
			c.Request.URL.RawQuery = ""
			c.Request.RequestURI = c.Request.URL.Path
			c.Request.Header.Del("Cookie")
			c.Request.Header.Del("Authorization")
			c.Request.Header.Del("Referer")
			wb.ServePreview(c.Writer, privateRequest)
			c.Abort()
			return
		}
		if binding.Kind != browserorigin.Native || instances == nil || instances.accessService == nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		path := c.Request.URL.Path
		refresh := path == dedicatedAccessRefreshPath
		// Parent renewal uses a custom header so capabilities do not enter its URL.
		// A preflight carries no credentials and permits only that exact operation.
		if refresh && c.Request.Method == http.MethodOptions && c.GetHeader("Origin") == reg.ManagerOrigin() &&
			(c.GetHeader("Access-Control-Request-Method") == http.MethodGet || c.GetHeader("Access-Control-Request-Method") == http.MethodHead) {
			c.Header("Access-Control-Allow-Origin", reg.ManagerOrigin())
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods", "GET, HEAD")
			c.Header("Access-Control-Allow-Headers", dedicatedAccessRefreshHeader)
			c.Header("Vary", "Origin")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		token := strings.TrimSpace(c.Query("token"))
		if token == "" && refresh {
			token = strings.TrimSpace(c.GetHeader(dedicatedAccessRefreshHeader))
		}
		bootstrap := false
		if token != "" {
			access, validationErr := instances.accessService.ValidateToken(token)
			if validationErr == nil && access.InstanceID == binding.InstanceID {
				runtimeType, managed := services.NormalizeV2RuntimeType(access.InstanceType)
				bootstrap = managed && runtimeType == binding.Target
			}
		}
		allowed := browserorigin.AllowNativeRequest(c.Request, reg.URL(binding), reg.ManagerOrigin(), bootstrap)
		c.Request = c.Request.WithContext(browserorigin.WithNativeContext(c.Request.Context()))
		c.Request.Header.Set(services.DedicatedRuntimeOriginHeader, binding.Target)
		prefix := "/api/v1/instances/" + strconv.Itoa(binding.InstanceID) + "/proxy"
		if path == "" {
			path = "/"
		}
		c.Request.URL.Path = prefix + path
		if c.Request.URL.RawPath != "" {
			c.Request.URL.RawPath = prefix + c.Request.URL.RawPath
		}
		c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(binding.InstanceID)}}
		redactInstanceProxyCredentials(c)
		if !allowed {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if refresh && c.GetHeader("Origin") == reg.ManagerOrigin() {
			c.Header("Access-Control-Allow-Origin", reg.ManagerOrigin())
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		}
		instances.ProxyInstance(c)
		c.Abort()
	}
}

// Permanent assignments cannot be reclaimed by waiting or removing instances.
func browserOriginCapacityFailure(c *gin.Context, err error) bool {
	if !errors.Is(err, browserorigin.ErrCapacity) {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{
		"success":    false,
		"error_code": "browser_origin_capacity_exhausted",
		"error":      "The permanent browser-origin pool or per-instance target limit is exhausted. Reuse an existing assigned target, or contact the administrator to expand the port pool if the instance target limit has not been reached.",
	})
	return true
}

func instanceAccessURLFailure(c *gin.Context, err error) {
	if !browserOriginCapacityFailure(c, err) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Unable to generate access URL"})
	}
}
