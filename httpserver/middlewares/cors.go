package middlewares

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const defaultAllowCredentials = "true"

var (
	defaultAllowedHeaders = []string{
		"Content-Type",
		"Content-Length",
		"Accept-Encoding",
		"X-CSRF-Token",
		"Authorization",
		"Accept",
		"Origin",
		"Cache-Control",
		"X-Requested-With",
	}

	defaultAllowedMethods = []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	}
)

// toHeaderCase converts lowercase strings to proper HTTP header case format
func toHeaderCase(s string) string {
	// Split by hyphen and capitalize each part
	parts := strings.Split(s, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		// Capitalize only the first letter, keep the rest as is
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, "-")
}

// CORS returns a Gin middleware that sets CORS headers, restricted to the
// origins in allowedOrigins. An Origin outside that list gets no CORS
// headers at all, so the browser blocks the response client credentials
// (cookies) travel with every request now, so Access-Control-Allow-Origin
// can no longer reflect-any-origin the way it could when only a
// self-attached Authorization header was at stake.
//
// If customCors is provided, it takes over entirely and allowedOrigins is
// ignored (kept for callers with a fully custom header set already wired
// up e.g. non-production/test configurations).
func CORS(allowedOrigins []string, customCors ...map[string]string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = true
	}

	return func(c *gin.Context) {
		// Apply custom headers if provided
		if len(customCors) > 0 {
			for key, value := range customCors[0] {
				c.Writer.Header().Set(toHeaderCase(key), value)
			}
		} else {
			origin := c.Request.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				c.Writer.Header().Set("Vary", "Origin")
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", defaultAllowCredentials)
				c.Writer.Header().Set("Access-Control-Allow-Headers", strings.Join(defaultAllowedHeaders, ", "))
				c.Writer.Header().Set("Access-Control-Allow-Methods", strings.Join(defaultAllowedMethods, ", "))
			}
		}

		// Handle preflight request
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
