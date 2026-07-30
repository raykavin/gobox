package middlewares

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/oidcauth"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// fakeVerifier lets tests control what Authorization's call to Verify
// returns, without touching a real OIDC provider.
type fakeVerifier struct {
	wantToken string
	claims    oidcauth.Claims
	err       error
}

var _ TokenVerifier = (*fakeVerifier)(nil)

// HasRole implements [TokenVerifier].
func (f *fakeVerifier) HasRole(claims oidcauth.Claims, role string) bool {
	return false
}

func (f *fakeVerifier) Verify(_ context.Context, token string) (oidcauth.Claims, error) {
	if f.wantToken != "" && token != f.wantToken {
		return oidcauth.Claims{}, errors.New("unexpected token")
	}
	return f.claims, f.err
}

// runAuthorization builds a one-route Gin engine guarded by Authorization,
// performs a single request against it configured by configureReq, and
// returns the response plus the claims the downstream handler observed via
// ClaimsFromContext (zero value if the middleware aborted first).
func runAuthorization(t *testing.T, verifier TokenVerifier, configureReq func(*http.Request)) (*httptest.ResponseRecorder, oidcauth.Claims, bool) {
	t.Helper()

	var gotClaims oidcauth.Claims
	var gotOK bool

	engine := gin.New()
	engine.Use(Authorization(verifier))
	engine.GET("/protected", func(ctx *gin.Context) {
		gotClaims, gotOK = ClaimsFromContext(ctx)
		ctx.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if configureReq != nil {
		configureReq(req)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	return rec, gotClaims, gotOK
}

func TestAuthorization_ValidBearerHeader(t *testing.T) {
	verifier := &fakeVerifier{wantToken: "tok-123", claims: oidcauth.Claims{Sub: "user-1"}}

	rec, claims, ok := runAuthorization(t, verifier, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer tok-123")
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !ok {
		t.Fatal("ClaimsFromContext ok = false, want true")
	}
	if claims.Sub != "user-1" {
		t.Errorf("claims.Sub = %q, want %q", claims.Sub, "user-1")
	}
}

func TestAuthorization_ValidCookie(t *testing.T) {
	verifier := &fakeVerifier{wantToken: "tok-cookie", claims: oidcauth.Claims{Sub: "user-2"}}

	rec, claims, ok := runAuthorization(t, verifier, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: "tok-cookie"})
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !ok || claims.Sub != "user-2" {
		t.Fatalf("claims = %+v, ok = %v, want Sub=user-2, ok=true", claims, ok)
	}
}

func TestAuthorization_RejectsChunkedTokenReassembly(t *testing.T) {
	full := "part-one-part-two-part-three"
	chunks := []string{"part-one-", "part-two-", "part-three"}
	verifier := &fakeVerifier{wantToken: full, claims: oidcauth.Claims{Sub: "user-4"}}

	rec, claims, ok := runAuthorization(t, verifier, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: AccessTokenCookie + accessTokenChunksCountSuffix, Value: strconv.Itoa(len(chunks))})
		for i, c := range chunks {
			name := AccessTokenCookie + accessTokenChunkSeparator + strconv.Itoa(i)
			r.AddCookie(&http.Cookie{Name: name, Value: url.PathEscape(c)})
		}
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !ok || claims.Sub != "user-4" {
		t.Fatalf("claims = %+v, ok = %v, want Sub=user-4, ok=true", claims, ok)
	}
}

func TestAuthorization_MissingChunkFailsClosed(t *testing.T) {
	verifier := &fakeVerifier{claims: oidcauth.Claims{Sub: "should-not-be-reached"}}

	rec, _, ok := runAuthorization(t, verifier, func(r *http.Request) {
		// Claims 3 chunks but only provides 2: reassembly must not proceed
		// with a truncated value.
		r.AddCookie(&http.Cookie{Name: AccessTokenCookie + accessTokenChunksCountSuffix, Value: "3"})
		r.AddCookie(&http.Cookie{Name: AccessTokenCookie + accessTokenChunkSeparator + "0", Value: "a"})
	})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
	if ok {
		t.Fatal("ClaimsFromContext ok = true, want false")
	}
}

func TestAuthorization_BothHeaderAndCookieIsForbidden(t *testing.T) {
	verifier := &fakeVerifier{claims: oidcauth.Claims{Sub: "should-not-be-reached"}}

	rec, _, ok := runAuthorization(t, verifier, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: "tok-cookie"})
		r.Header.Set("Authorization", "Bearer tok-header")
	})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
	if ok {
		t.Fatal("ClaimsFromContext ok = true, want false")
	}
}

func TestAuthorization_MissingTokenIsUnauthorized(t *testing.T) {
	verifier := &fakeVerifier{}

	rec, _, ok := runAuthorization(t, verifier, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
	if ok {
		t.Fatal("ClaimsFromContext ok = true, want false")
	}
}

func TestAuthorization_MalformedHeaderIsUnauthorized(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"no scheme", "tok-123"},
		{"wrong scheme", "Basic tok-123"},
		{"empty token", "Bearer "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verifier := &fakeVerifier{}
			rec, _, ok := runAuthorization(t, verifier, func(r *http.Request) {
				r.Header.Set("Authorization", tc.header)
			})
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
			}
			if ok {
				t.Fatal("ClaimsFromContext ok = true, want false")
			}
		})
	}
}

func TestAuthorization_VerifyFailureIsUnauthorized(t *testing.T) {
	verifier := &fakeVerifier{wantToken: "tok-123", err: errors.New("token expired")}

	rec, _, ok := runAuthorization(t, verifier, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer tok-123")
	})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
	}
	if ok {
		t.Fatal("ClaimsFromContext ok = true, want false")
	}
}
