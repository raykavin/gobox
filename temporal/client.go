package temporal

import (
	"errors"
	"net"
	"strconv"
	"time"

	"go.temporal.io/sdk/client"
)

// Sentinel errors returned when dialing the Temporal client.
var (
	ErrEmptyHost      = errors.New("temporal host cannot be empty")
	ErrEmptyNamespace = errors.New("temporal namespace cannot be empty")
)

// Defaults applied by DefaultActivityOpts when no option overrides them.
const (
	defaultMaxAttempts        = 3
	defaultInitialInterval    = 5 * time.Second
	defaultMaximumInterval    = time.Minute
	defaultBackoffCoefficient = 2.0
	defaultStartToClose       = 1 * time.Hour
)

// NewClient dials a Temporal server and returns a connected client. Host and
// namespace are required.
func NewClient(namespace, host string, port uint16) (client.Client, error) {
	if host == "" {
		return nil, ErrEmptyHost
	}
	if namespace == "" {
		return nil, ErrEmptyNamespace
	}

	p := strconv.FormatUint(uint64(port), 10)

	opts := client.Options{
		HostPort:  net.JoinHostPort(host, p),
		Namespace: namespace,
	}

	return client.Dial(opts)
}
