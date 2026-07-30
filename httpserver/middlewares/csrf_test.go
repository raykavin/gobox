package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func runCSRF(t *testing.T, method string, configureReq func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	engine := gin.New()
	engine.Use(CSRF())
	engine.Handle(method, "/protected", func(ctx *gin.Context) {
		ctx.Status(http.StatusOK)
	})

	req := httptest.NewRequest(method, "/protected", nil)
	if configureReq != nil {
		configureReq(req)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestCSRF_SafeMethodsAreExempt(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		rec := runCSRF(t, method, nil)
		if rec.Code == http.StatusForbidden {
			t.Errorf("method %s: got 403, want it exempt from CSRF", method)
		}
	}
}

func TestCSRF_MutatingRequestWithMatchingTokenPasses(t *testing.T) {
	rec := runCSRF(t, http.MethodPost, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: CSRFCookie, Value: "secret-token"})
		r.Header.Set(CSRFHeader, "secret-token")
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestCSRF_MissingCookieIsForbidden(t *testing.T) {
	rec := runCSRF(t, http.MethodPost, func(r *http.Request) {
		r.Header.Set(CSRFHeader, "secret-token")
	})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestCSRF_MissingHeaderIsForbidden(t *testing.T) {
	rec := runCSRF(t, http.MethodPost, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: CSRFCookie, Value: "secret-token"})
	})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestCSRF_MismatchedTokenIsForbidden(t *testing.T) {
	rec := runCSRF(t, http.MethodPost, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: CSRFCookie, Value: "secret-token"})
		r.Header.Set(CSRFHeader, "different-token")
	})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
}
