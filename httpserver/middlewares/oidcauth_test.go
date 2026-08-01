package middlewares

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// newCookieTestContext builds a gin.Context whose request carries incoming
// (containing cookies) and whose ResponseRecorder captures every Set-Cookie
// the handler under test writes.
func newCookieTestContext(t *testing.T, incoming []*http.Cookie) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range incoming {
		ctx.Request.AddCookie(c)
	}
	return ctx, rec
}

// setCookieMap indexes a recorder's Set-Cookie headers by name for
// convenient lookup in assertions.
func setCookiesByName(rec *httptest.ResponseRecorder) map[string]*http.Cookie {
	out := make(map[string]*http.Cookie)
	for _, c := range rec.Result().Cookies() {
		out[c.Name] = c
	}
	return out
}

func TestSetChunkedCookie_SmallValueWritesSingleCookie(t *testing.T) {
	h := &Auth{cookies: AuthCookieOptions{Path: "/"}}
	ctx, rec := newCookieTestContext(t, nil)

	h.setChunkedCookie(ctx, AccessTokenCookie, "short-token", time.Minute, true)

	cookies := setCookiesByName(rec)
	got, ok := cookies[AccessTokenCookie]
	if !ok || got.Value != "short-token" {
		t.Fatalf("cookies[%q] = %+v, ok=%v, want value %q", AccessTokenCookie, got, ok, "short-token")
	}
	// A clearing Set-Cookie for the count is fine (idempotent no-op when
	// there was nothing to clear); it must just not carry a real count.
	if c, ok := cookies[AccessTokenCookie+accessTokenChunksCountSuffix]; ok && c.Value != "" {
		t.Fatalf("chunk-count cookie = %+v, want cleared (empty value)", c)
	}
}

func TestSetChunkedCookie_LargeValueSplitsAndReassembles(t *testing.T) {
	h := &Auth{cookies: AuthCookieOptions{Path: "/"}}
	ctx, rec := newCookieTestContext(t, nil)

	value := strings.Repeat("a", maxCookieValueBytes*2+123)
	h.setChunkedCookie(ctx, AccessTokenCookie, value, time.Minute, true)

	cookies := setCookiesByName(rec)
	// The plain cookie is actively cleared (not simply absent), so
	// readAccessTokenCookie's unchunked branch never picks up a stale value.
	if plain, ok := cookies[AccessTokenCookie]; ok && plain.Value != "" {
		t.Fatalf("plain %q cookie = %+v, want cleared (empty value)", AccessTokenCookie, plain)
	}

	countCookie, ok := cookies[AccessTokenCookie+accessTokenChunksCountSuffix]
	if !ok {
		t.Fatalf("expected a chunk-count cookie")
	}
	count, err := strconv.Atoi(countCookie.Value)
	if err != nil || count != 3 {
		t.Fatalf("chunk count = %q, want 3", countCookie.Value)
	}

	// Reassemble the way readAccessTokenCookie does, against a fresh request
	// carrying only what setChunkedCookie wrote — proving the write side
	// produces exactly what the existing read side expects.
	readCtx, _ := newCookieTestContext(t, rec.Result().Cookies())
	got, ok := readAccessTokenCookie(readCtx)
	if !ok {
		t.Fatalf("readAccessTokenCookie ok = false, want true")
	}
	if got != value {
		t.Fatalf("reassembled value length = %d, want %d", len(got), len(value))
	}
}

func TestSetChunkedCookie_ShrinkingValueClearsStaleChunks(t *testing.T) {
	h := &Auth{cookies: AuthCookieOptions{Path: "/"}}

	// Simulate a browser that already holds 3 chunks from a previous, larger
	// token.
	incoming := []*http.Cookie{
		{Name: AccessTokenCookie + accessTokenChunksCountSuffix, Value: "3"},
		{Name: AccessTokenCookie + accessTokenChunkSeparator + "0", Value: "a"},
		{Name: AccessTokenCookie + accessTokenChunkSeparator + "1", Value: "b"},
		{Name: AccessTokenCookie + accessTokenChunkSeparator + "2", Value: "c"},
	}
	ctx, rec := newCookieTestContext(t, incoming)

	h.setChunkedCookie(ctx, AccessTokenCookie, "small-again", time.Minute, true)

	cookies := setCookiesByName(rec)
	got, ok := cookies[AccessTokenCookie]
	if !ok || got.Value != "small-again" {
		t.Fatalf("cookies[%q] = %+v, ok=%v, want value %q", AccessTokenCookie, got, ok, "small-again")
	}
	if c, ok := cookies[AccessTokenCookie+accessTokenChunksCountSuffix]; !ok || c.MaxAge >= 0 {
		t.Fatalf("expected the stale chunk-count cookie to be cleared, got %+v (ok=%v)", c, ok)
	}
	for i := range 3 {
		name := AccessTokenCookie + accessTokenChunkSeparator + strconv.Itoa(i)
		c, ok := cookies[name]
		if !ok || c.MaxAge >= 0 {
			t.Fatalf("expected stale chunk %q to be cleared, got %+v (ok=%v)", name, c, ok)
		}
	}
}

func TestClearChunkedCookie_ClearsAllChunksAndCount(t *testing.T) {
	h := &Auth{cookies: AuthCookieOptions{Path: "/"}}

	incoming := []*http.Cookie{
		{Name: AccessTokenCookie + accessTokenChunksCountSuffix, Value: "2"},
		{Name: AccessTokenCookie + accessTokenChunkSeparator + "0", Value: "a"},
		{Name: AccessTokenCookie + accessTokenChunkSeparator + "1", Value: "b"},
	}
	ctx, rec := newCookieTestContext(t, incoming)

	h.clearChunkedCookie(ctx, AccessTokenCookie)

	cookies := setCookiesByName(rec)
	for _, name := range []string{
		AccessTokenCookie,
		AccessTokenCookie + accessTokenChunksCountSuffix,
		AccessTokenCookie + accessTokenChunkSeparator + "0",
		AccessTokenCookie + accessTokenChunkSeparator + "1",
	} {
		c, ok := cookies[name]
		if !ok || c.MaxAge >= 0 {
			t.Fatalf("expected %q to be cleared, got %+v (ok=%v)", name, c, ok)
		}
	}
}
