// Package temporal provides a thin wrapper around the Temporal Go SDK for
// dialling a client and building activity options with sensible defaults.
//
// # Connecting
//
//	c, err := temporal.NewClient("default", "localhost", 7233)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer c.Close()
//
// # Activity options
//
// DefaultActivityOpts returns activity options with a 1 h StartToClose timeout
// and a capped exponential-backoff retry policy (3 attempts, 5 s initial
// interval, 1 min cap). Pass functional options to override specific fields.
//
//	opts := temporal.DefaultActivityOpts(
//	    temporal.WithStartToCloseTimeout(30 * time.Minute),
//	    temporal.WithMaximumAttempts(5),
//	)
//	ctx = workflow.WithActivityOptions(ctx, opts)
//	workflow.ExecuteActivity(ctx, myActivity, input)
//
// # Config struct
//
// TemporalConfig carries the four connection parameters and is intended for
// use with the config package (mapstructure tags are included for Viper).
//
//	var cfg temporal.TemporalConfig
//	// populate from Viper / env ...
//	c, err := temporal.NewClient(cfg.Namespace, cfg.Host, cfg.Port)
package temporal
