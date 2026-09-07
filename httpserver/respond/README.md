# httpserver/respond

The `respond` package writes a consistent JSON envelope for Gin handlers, and maps domain sentinel errors to the HTTP status and error code the client sees. It is intended for services that want every endpoint to answer with the same shape, and want the translation from `error` to HTTP response declared once at startup rather than repeated in every handler.

## Import

```go
import "github.com/raykavin/gobox/httpserver/respond"
```

## What it provides

- one `Response` envelope for every reply, successful or not
- status helpers for the common codes, from `OK` through `ServiceUnavailable`
- `APIError` and `NewError` for machine-readable error codes with optional details
- `ErrorResolver` for mapping sentinel errors to status, code, and user-facing message

## The envelope

```go
type Response struct {
    Success bool        `json:"success"`
    Message string      `json:"message,omitempty"`
    Data    any         `json:"data,omitempty"`
    Errors  []APIError `json:"errors,omitempty"`
}
```

A success carries `Data`, a failure carries `Errors`, and both carry a `Message`. Each entry in `Errors` is an `APIError`:

```go
type APIError struct {
    Code    string `json:"code"`    // stable, machine-readable
    Message string `json:"message"` // user-facing
    Details any    `json:"details,omitempty"`
}
```

## Sending responses

```go
func (h *Handler) Get(c *gin.Context) {
    user, err := h.users.ByID(c, c.Param("id"))
    if err != nil {
        respond.NotFound(c, respond.NewError("ERR_USER_NOT_FOUND", "User not found"))
        return
    }
    respond.OK(c, user)
}
```

Every success helper takes an optional trailing message that overrides the default:

```go
respond.Created(c, user, "Account created, check your email to confirm")
```

### Helpers

| Helper | Status | Signature |
|---|---|---|
| `OK` | 200 | `(ctx, data, message ...string)` |
| `Created` | 201 | `(ctx, data, message ...string)` |
| `Accepted` | 202 | `(ctx, data, message ...string)` |
| `NoContent` | 204 | `(ctx)` |
| `BadRequest` | 400 | `(ctx, errs ...APIError)` |
| `Unauthorized` | 401 | `(ctx, errs ...APIError)` |
| `Forbidden` | 403 | `(ctx, errs ...APIError)` |
| `NotFound` | 404 | `(ctx, errs ...APIError)` |
| `Conflict` | 409 | `(ctx, errs ...APIError)` |
| `UnprocessableEntity` | 422 | `(ctx, errs ...APIError)` |
| `TooManyRequests` | 429 | `(ctx, errs ...APIError)` |
| `InternalServerError` | 500 | `(ctx, errs ...APIError)` |
| `ServiceUnavailable` | 503 | `(ctx, errs ...APIError)` |
| `Error` | caller's | `(ctx, status, errs ...APIError)` |

Success helpers call `ctx.JSON`. Error helpers call `ctx.AbortWithStatusJSON`, so they also stop the middleware chain.

## Mapping errors with ErrorResolver

Register the mapping once at startup, then let handlers hand any error to the resolver:

```go
resolver := respond.NewErrorResolver()

resolver.MustAddEntries(
    respond.Err{
        Err:     user.ErrNotFound,
        Status:  http.StatusNotFound,
        Code:    "ERR_USER_NOT_FOUND",
        Message: "User not found",
    },
    respond.Err{
        Err:     user.ErrEmailTaken,
        Status:  http.StatusConflict,
        Code:    "ERR_EMAIL_TAKEN",
        Message: "That email address is already registered",
    },
)
```

```go
func (h *Handler) Create(c *gin.Context) {
    user, err := h.users.Create(c, input)
    if err != nil {
        apiErr, status, known := resolver.Resolve(err)
        if !known {
            h.log.WithError(err).Error("unmapped error")
        }
        respond.Error(c, status, apiErr)
        return
    }
    respond.Created(c, user)
}
```

### Err

| Field | Description |
|---|---|
| `Err` | The sentinel, matched with `errors.Is` |
| `Status` | HTTP status returned to the client; a zero value is normalized to 500 at registration |
| `Code` | Stable machine-readable identifier |
| `Message` | User-facing text |
| `Details` | Optional static context; must be safe for concurrent reads |

### ErrorResolver methods

| Method | Description |
|---|---|
| `NewErrorResolver() *ErrorResolver` | A resolver holding only the generic 500 fallback |
| `AddEntries(...Err) error` | Adds mappings, returning an error on an invalid or duplicate entry |
| `MustAddEntries(...Err)` | Same, but panics; for bootstrap code where a bad mapping is a bug |
| `Resolve(err) (APIError, int, bool)` | Always renderable: the mapping if known, the fallback otherwise. The bool reports whether the error was known |
| `ResolveWithDetails(err, details) (APIError, int, bool)` | `Resolve` with per-request details replacing the static ones |
| `Lookup(err) (Err, bool)` | The first entry whose sentinel matches |
| `SetFallback(Err) error` | Replaces the response used when nothing matches |
| `Len() int` | Number of registered mappings |

## Notes

- match priority follows registration order, so register specific errors before the generic ones they wrap
- registration is expected to happen once during startup; lookups are safe for concurrent use afterwards
- `AddEntries` rejects a sentinel that is already mapped, which turns a duplicate registration into a loud startup failure rather than a silently shadowed entry
- `Resolve` never fails: the third return value distinguishes a real mapping from the fallback, which is the signal to use when deciding whether to log at error or debug level
- use `ResolveWithDetails` to attach per-request context such as offending field names; `Err.Details` is shared across every request that hits that mapping, so never mutate it
- `NoContent` writes no body at all, so the envelope does not apply to it
