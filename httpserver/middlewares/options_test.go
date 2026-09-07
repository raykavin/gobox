package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The existing cors_test.go and csrf_test.go cover the default behaviour these
// middlewares had before they took options. What follows covers the options
// themselves: that a caller's policy is what gets applied, and that the
// built-in defaults only fill genuine gaps.

func TestCORSWithOptions_AppliesTheGivenPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(CORSWithOptions(CORSOptions{
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}))
	engine.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Errorf("Allow-Methods = %q, want %q", got, "GET, POST")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
		t.Errorf("Allow-Headers = %q, want %q", got, "Content-Type")
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}
}

func TestCORSWithOptions_EmptyListsFallBackToDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(CORSWithOptions(CORSOptions{AllowedOrigins: []string{"https://a.test"}}))
	engine.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://a.test")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("expected the default method set, got nothing")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("expected the default header set, got nothing")
	}
}

// Omitting the header is not the same as sending "false": the fetch spec only
// recognises the header's presence, so "false" would still allow credentials.
func TestCORSWithOptions_CredentialsOffOmitsTheHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(CORSWithOptions(CORSOptions{AllowedOrigins: []string{"https://a.test"}}))
	engine.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Origin", "https://a.test")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if got, ok := rec.Header()["Access-Control-Allow-Credentials"]; ok {
		t.Errorf("header present with %v, want it omitted entirely", got)
	}
}

func TestCSRFWithOptions_UsesTheGivenNames(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(CSRFWithOptions(CSRFOptions{CookieName: "custom_c", HeaderName: "X-Custom-H"}))
	engine.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func(cookie, header string) int {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.AddCookie(&http.Cookie{Name: cookie, Value: "tok"})
		req.Header.Set(header, "tok")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := call("custom_c", "X-Custom-H"); got != http.StatusOK {
		t.Errorf("configured pair got %d, want 200", got)
	}
	if got := call(CSRFCookie, CSRFHeader); got != http.StatusForbidden {
		t.Errorf("default pair got %d, want 403 once other names are configured", got)
	}
}

func TestCSRFWithOptions_EmptyNamesFallBackToDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(CSRFWithOptions(CSRFOptions{}))
	engine.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookie, Value: "tok"})
	req.Header.Set(CSRFHeader, "tok")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200 with the default names", rec.Code)
	}
}

func TestRateLimitByIP_AllowsBurstThenRefuses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	// One request per minute sustained, so only the burst is available within
	// the test's lifetime.
	engine.Use(RateLimitByIP(RateLimitOptions{RequestsPerMinute: 1, Burst: 2}))
	engine.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	codes := make([]int, 0, 3)
	for range 3 {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
		codes = append(codes, rec.Code)
	}

	want := []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}
	for i, code := range codes {
		if code != want[i] {
			t.Errorf("request %d got %d, want %d (all: %v)", i+1, code, want[i], codes)
		}
	}
}

func TestRateLimitOptions_WithDefaultsOnlyFillsGaps(t *testing.T) {
	got := RateLimitOptions{RequestsPerMinute: 0, Burst: 7}.WithDefaults(20, 5)
	if got.RequestsPerMinute != 20 {
		t.Errorf("RequestsPerMinute = %d, want the default 20", got.RequestsPerMinute)
	}
	if got.Burst != 7 {
		t.Errorf("Burst = %d, want the configured 7 to be kept", got.Burst)
	}
}
