package middlewares

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/raykavin/gobox/oidcauth"
)

// roleVerifier grants exactly the roles it was built with, so a test can state
// what a caller holds and nothing else.
type roleVerifier struct {
	granted map[string]bool
}

var _ TokenVerifier = (*roleVerifier)(nil)

func newRoleVerifier(roles ...string) *roleVerifier {
	granted := make(map[string]bool, len(roles))
	for _, role := range roles {
		granted[role] = true
	}
	return &roleVerifier{granted: granted}
}

func (v *roleVerifier) Verify(context.Context, string) (oidcauth.Claims, error) {
	return oidcauth.Claims{}, nil
}

func (v *roleVerifier) HasRole(_ oidcauth.Claims, role string) bool { return v.granted[role] }

// runGuard puts claims in the context the way Authorization would, then runs
// the guard, and reports the status and whether the handler behind it ran.
func runGuard(t *testing.T, guard gin.HandlerFunc) (int, bool, *gin.Context) {
	t.Helper()

	var reached bool
	var seen *gin.Context

	engine := gin.New()
	engine.Use(func(ctx *gin.Context) {
		ctx.Set(claimsKey, oidcauth.Claims{})
		ctx.Next()
	})
	engine.GET("/protected", guard, func(ctx *gin.Context) {
		reached = true
		seen = ctx
		ctx.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/protected", nil))

	return rec.Code, reached, seen
}

func TestRequireRole_GrantsTheHolderAndRefusesEveryoneElse(t *testing.T) {
	for _, tc := range []struct {
		name       string
		held       []string
		required   string
		wantStatus int
		wantRun    bool
	}{
		{
			name:       "holder is let through",
			held:       []string{"expenses:create"},
			required:   "expenses:create",
			wantStatus: http.StatusOK,
			wantRun:    true,
		},
		{
			name:       "caller holding nothing is refused",
			held:       nil,
			required:   "expenses:create",
			wantStatus: http.StatusForbidden,
			wantRun:    false,
		},
		{
			name: "a neighbouring permission does not open the route",
			// The whole point of atomic permissions: holding the read right on
			// the same resource must not open a write route.
			held:       []string{"expenses:read", "expenses:update"},
			required:   "expenses:create",
			wantStatus: http.StatusForbidden,
			wantRun:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, ran, _ := runGuard(t, RequireRole(newRoleVerifier(tc.held...), tc.required))

			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d", status, tc.wantStatus)
			}
			if ran != tc.wantRun {
				t.Fatalf("handler reached = %v, want %v", ran, tc.wantRun)
			}
		})
	}
}

func TestRequireRole_MissingClaimsIsUnauthorized(t *testing.T) {
	engine := gin.New()
	engine.GET("/protected", RequireRole(newRoleVerifier("expenses:read"), "expenses:read"),
		func(ctx *gin.Context) { ctx.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/protected", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireRole_OptionalRoleContextsOnlyApplyToTheirHolder(t *testing.T) {
	extra := RoleContext{
		RoleName: "payment-batch:report:private:read",
		Values:   map[string]any{"can_read_private": true},
	}

	t.Run("held", func(t *testing.T) {
		verifier := newRoleVerifier("payment-batch:report:read", "payment-batch:report:private:read")
		status, _, ctx := runGuard(t, RequireRole(verifier, "payment-batch:report:read", extra))

		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if value, _ := ctx.Get("can_read_private"); value != true {
			t.Fatalf("context flag = %v, want true", value)
		}
	})

	t.Run("not held", func(t *testing.T) {
		// The route still opens the extra permission is additive but the
		// flag it controls must stay unset, or the private column leaks to a
		// caller holding only the plain read permission.
		verifier := newRoleVerifier("payment-batch:report:read")
		status, _, ctx := runGuard(t, RequireRole(verifier, "payment-batch:report:read", extra))

		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if _, ok := ctx.Get("can_read_private"); ok {
			t.Fatal("context flag was set for a caller that does not hold the permission")
		}
	})
}

func TestRequireAnyRole(t *testing.T) {
	const (
		approve = "payment-batch:approve"
		revoke  = "payment-batch:approval:revoke"
	)
	required := []string{approve, revoke}

	for _, tc := range []struct {
		name       string
		held       []string
		wantStatus int
	}{
		{name: "first permission opens it", held: []string{approve}, wantStatus: http.StatusOK},
		{name: "second permission opens it", held: []string{revoke}, wantStatus: http.StatusOK},
		{name: "both is fine", held: required, wantStatus: http.StatusOK},
		{name: "neither is refused", held: []string{"payment-batch:read"}, wantStatus: http.StatusForbidden},
		{name: "holding nothing is refused", held: nil, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _, _ := runGuard(t, RequireAnyRole(newRoleVerifier(tc.held...), required))

			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d", status, tc.wantStatus)
			}
		})
	}
}

// TestRequireAnyRole_EmptySetDeniesEveryone pins the safe reading of "any of
// nothing": a guard configured with no permissions must refuse, never wave
// callers through.
func TestRequireAnyRole_EmptySetDeniesEveryone(t *testing.T) {
	status, ran, _ := runGuard(t, RequireAnyRole(newRoleVerifier("expenses:read"), nil))

	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
	}
	if ran {
		t.Fatal("handler ran behind a guard that grants no permission")
	}
}
