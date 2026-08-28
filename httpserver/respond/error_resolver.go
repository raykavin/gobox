package respond

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// Err binds a sentinel error to the representation returned to API clients.
// A zero Status is normalized to 500 during registration.
type Err struct {
	Err     error  // sentinel matched with errors.Is
	Status  int    // HTTP status returned to the client
	Code    string // stable machine-readable identifier
	Message string // user-facing text
	Details any    // optional static context; must be safe for concurrent reads
}

// IsErr reports whether err matches this entry's sentinel.
func (e Err) IsErr(err error) bool {
	return errors.Is(err, e.Err)
}

func (e Err) validate() error {
	switch {
	case e.Err == nil:
		return errors.New("error cannot be nil")
	case e.Code == "":
		return errors.New("code cannot be empty")
	case e.Message == "":
		return errors.New("message cannot be empty")
	case e.Status != 0 && (e.Status < 100 || e.Status > 599):
		return fmt.Errorf("invalid http status %d", e.Status)
	}
	return nil
}

func (e Err) apiError() APIError {
	return APIError{
		Code:    e.Code,
		Message: e.Message,
		Details: e.Details,
	}
}

// ErrorResolver maps sentinel errors to API errors. Registration is expected to
// happen once during startup; lookups are safe for concurrent use afterwards.
//
// Match priority follows registration order: register specific errors before
// the generic ones they wrap.
type ErrorResolver struct {
	mu       sync.RWMutex
	entries  []Err
	fallback Err
}

// NewErrorResolver returns a register with a generic 500 fallback.
func NewErrorResolver() *ErrorResolver {
	return &ErrorResolver{
		entries: make([]Err, 0, 32),
		fallback: Err{
			Err:     errors.New("respond: unregistered error"),
			Status:  http.StatusInternalServerError,
			Code:    "INTERNAL_SERVER_ERROR",
			Message: msgInternalError,
		},
	}
}

// SetFallback replaces the response used when no sentinel matches.
func (r *ErrorResolver) SetFallback(e Err) error {
	if err := e.validate(); err != nil {
		return fmt.Errorf("respond err register: fallback: %w", err)
	}
	if e.Status == 0 {
		e.Status = http.StatusInternalServerError
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = e

	return nil
}

// AddEntries adds one or more mappings. It fails on invalid entries and on
// sentinels that are already mapped, so startup misconfiguration is loud.
func (r *ErrorResolver) AddEntries(entries ...Err) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range entries {
		if err := e.validate(); err != nil {
			return fmt.Errorf("add error respond: entry %d: %w", i, err)
		}
		if dup, ok := r.aliasLocked(e.Err); ok {
			return fmt.Errorf(
				"add error respond: entry %d (%s): sentinel already mapped to %q",
				i, e.Code, dup.Code,
			)
		}
		if e.Status == 0 {
			e.Status = http.StatusInternalServerError
		}
		r.entries = append(r.entries, e)
	}

	return nil
}

// MustAddEntries uses in init/bootstrap code, where a bad mapping
// is a programming error and should stop the process.
func (r *ErrorResolver) MustAddEntries(entries ...Err) {
	if err := r.AddEntries(entries...); err != nil {
		panic(err)
	}
}

// Lookup returns the first entry whose sentinel matches err.
func (r *ErrorResolver) Lookup(err error) (Err, bool) {
	if err == nil {
		return Err{}, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.IsErr(err) {
			return e, true
		}
	}

	return Err{}, false
}

// Resolve always returns a renderable response: the registered mapping when
// there is one, the fallback otherwise. The bool reports whether err was known,
// which is useful to decide between logging at error or debug level.
func (r *ErrorResolver) Resolve(err error) (APIError, int, bool) {
	if e, ok := r.Lookup(err); ok {
		return e.apiError(), e.Status, true
	}

	r.mu.RLock()
	fallback := r.fallback
	r.mu.RUnlock()

	return fallback.apiError(), fallback.Status, false
}

// ResolveWithDetails is Resolve with per-request details overriding the static
// ones (validation fields, offending IDs, etc.).
func (r *ErrorResolver) ResolveWithDetails(err error, details any) (APIError, int, bool) {
	apiErr, status, ok := r.Resolve(err)
	if details != nil {
		apiErr.Details = details
	}

	return apiErr, status, ok
}

// Len returns the number of registered mappings.
func (r *ErrorResolver) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.entries)
}

// aliasLocked reports an entry that is mutually equivalent to err, i.e. an
// actual re-registration rather than a specialization of an already mapped
// error. Caller must hold the lock.
func (r *ErrorResolver) aliasLocked(err error) (Err, bool) {
	for _, e := range r.entries {
		if errors.Is(err, e.Err) && errors.Is(e.Err, err) {
			return e, true
		}
	}

	return Err{}, false
}
