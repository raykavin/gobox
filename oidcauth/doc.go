// Package oidcauth provides OpenID Connect token verification with optional
// in-memory caching and role-based authorization helpers.
//
// # Basic usage
//
//	verifier, err := oidcauth.New(ctx, oidcauth.Config{
//	    RealmURL:     "https://keycloak.example.com/realms/main",
//	    ClientID:     "my-app",
//	    ClientSecret: "secret",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	claims, err := verifier.Verify(ctx, bearerToken)
//	if err != nil {
//	    // handle errors.Is(err, oidcauth.ErrTokenRevoked), etc.
//	}
//
// # Caching
//
// By default no cache is used and every call to Verify hits the provider.
// Attach a MemoryCache to avoid redundant network round-trips:
//
//	cache := oidcauth.NewMemoryCache(ctx, oidcauth.DefaultCacheDuration)
//	defer cache.Close()
//
//	verifier, err := oidcauth.New(ctx, config, oidcauth.WithCache(cache))
//
// Custom backends (Redis, Memcached, etc.) can be used by implementing the
// Cache interface:
//
//	type Cache interface {
//	    Get(key string, now time.Time) (Claims, bool)
//	    Set(key string, claims Claims, now time.Time)
//	}
//
// # Role-based authorization
//
// HasRole checks whether a token carries a specific Keycloak client role:
//
//	if !verifier.HasRole(claims, "admin") {
//	    http.Error(w, "forbidden", http.StatusForbidden)
//	    return
//	}
//
// # Error handling
//
// All errors wrap one of the package-level sentinels and can be inspected
// with errors.Is:
//
//	switch {
//	case errors.Is(err, oidcauth.ErrTokenRevoked):
//	    // token was revoked server-side
//	case errors.Is(err, oidcauth.ErrTokenValidationFailed):
//	    // signature / expiry / audience check failed
//	case errors.Is(err, oidcauth.ErrIntrospectionFailed):
//	    // could not reach the introspection endpoint
//	}
package oidcauth
