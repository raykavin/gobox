package fcm

import "errors"

var (
	// ErrInvalidCredentials is returned by New when the service account key
	// cannot be used to sign requests.
	ErrInvalidCredentials = errors.New("fcm: service account credentials are invalid")
	// ErrMissingProject is returned by New when neither the Config nor the
	// service account names a project.
	ErrMissingProject = errors.New("fcm: project id must not be empty")
	// ErrProjectMismatch is returned by New when the configured project is
	// not the service account's.
	ErrProjectMismatch = errors.New("fcm: project does not match the service account")
	// ErrEmptyAddress is returned by Send when the message has no
	// registration token.
	ErrEmptyAddress = errors.New("fcm: address must not be empty")
)
