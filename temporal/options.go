package temporal

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ActivityOption customizes the activity options produced by
// DefaultActivityOpts. Options are applied in order.
type ActivityOption func(*workflow.ActivityOptions)

// WithTaskQueue routes the activity to a specific task queue. An empty value
// leaves the workflow's default task queue in place.
func WithTaskQueue(taskQueue string) ActivityOption {
	return func(o *workflow.ActivityOptions) {
		o.TaskQueue = taskQueue
	}
}

// WithStartToCloseTimeout overrides the StartToClose timeout.
func WithStartToCloseTimeout(d time.Duration) ActivityOption {
	return func(o *workflow.ActivityOptions) {
		o.StartToCloseTimeout = d
	}
}

// WithScheduleToCloseTimeout sets the ScheduleToClose timeout (unset by
// default).
func WithScheduleToCloseTimeout(d time.Duration) ActivityOption {
	return func(o *workflow.ActivityOptions) {
		o.ScheduleToCloseTimeout = d
	}
}

// WithMaximumAttempts overrides the retry policy's maximum attempts. A value
// of 0 means unlimited retries, per the Temporal SDK.
func WithMaximumAttempts(n int32) ActivityOption {
	return func(o *workflow.ActivityOptions) {
		o.RetryPolicy.MaximumAttempts = n
	}
}

// WithRetryPolicy replaces the entire retry policy.
func WithRetryPolicy(p *temporal.RetryPolicy) ActivityOption {
	return func(o *workflow.ActivityOptions) {
		o.RetryPolicy = p
	}
}

// DefaultActivityOpts returns activity options with sensible defaults: a 1h
// StartToClose timeout and a capped exponential-backoff retry policy. Pass
// options to customize specific fields, e.g.:
//
//	opts := temporal.DefaultActivityOpts(
//		temporal.WithTaskQueue("audit"),
//		temporal.WithStartToCloseTimeout(30*time.Minute),
//	)
func DefaultActivityOpts(opts ...ActivityOption) workflow.ActivityOptions {
	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: defaultStartToClose,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    defaultInitialInterval,
			MaximumInterval:    defaultMaximumInterval,
			BackoffCoefficient: defaultBackoffCoefficient,
			MaximumAttempts:    defaultMaxAttempts,
		},
	}

	for _, opt := range opts {
		opt(&activityOpts)
	}

	return activityOpts
}
