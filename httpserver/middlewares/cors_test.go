package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func runCORS(t *testing.T, allowedOrigins []string, origin string) *httptest.ResponseRecorder {
	t.Helper()

	engine := gin.New()
	engine.Use(CORS(allowedOrigins))
	engine.GET("/resource", func(ctx *gin.Context) {
		ctx.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestCORS_AllowedOriginGetsCredentialedHeaders(t *testing.T) {
	rec := runCORS(t, []string{"https://app.example.com"}, "https://app.example.com")

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the allowed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
	}
}

func TestCORS_DisallowedOriginGetsNoHeaders(t *testing.T) {
	rec := runCORS(t, []string{"https://app.example.com"}, "https://evil.example.com")

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want empty for a disallowed origin", got)
	}
}

func TestCORS_PreflightIsHandled(t *testing.T) {
	engine := gin.New()
	engine.Use(CORS([]string{"https://app.example.com"}))
	engine.OPTIONS("/resource", func(ctx *gin.Context) {
		t.Fatal("preflight should be aborted before reaching the route handler")
	})

	req := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}
