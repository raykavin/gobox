# temporal

The `temporal` package provides a thin wrapper around the [Temporal Go SDK](https://github.com/temporalio/sdk-go) for dialling a client and building activity options with sensible defaults. It is intended for services that use Temporal for workflow orchestration and want a consistent way to connect and configure activities without repeating the same boilerplate in every project.

## Import

```go
import "github.com/raykavin/gobox/temporal"
```

## What it provides

- `NewClient()` for dialling a Temporal server with host, port, and namespace validation
- `TemporalConfig` for carrying connection parameters with Viper-compatible mapstructure tags
- `DefaultActivityOpts()` for building `workflow.ActivityOptions` with a sensible retry policy
- functional options for overriding specific activity fields: `WithTaskQueue`, `WithStartToCloseTimeout`, `WithScheduleToCloseTimeout`, `WithMaximumAttempts`, `WithRetryPolicy`

## Main types

- `TemporalConfig`: connection settings (`Host`, `Port`, `Namespace`, `TaskQueue`)
- `ActivityOption`: `func(*workflow.ActivityOptions)` applied by `DefaultActivityOpts`

## Connecting

```go
package main

import (
    "log"

    "github.com/raykavin/gobox/temporal"
)

func main() {
    c, err := temporal.NewClient("default", "localhost", 7233)
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    // use c as a normal temporal client.Client
}
```

## Activity options inside a workflow

```go
import (
    "time"

    "github.com/raykavin/gobox/temporal"
    "go.temporal.io/sdk/workflow"
)

func MyWorkflow(ctx workflow.Context, input string) error {
    opts := temporal.DefaultActivityOpts(
        temporal.WithStartToCloseTimeout(30 * time.Minute),
        temporal.WithMaximumAttempts(5),
        temporal.WithTaskQueue("heavy-processing"),
    )
    ctx = workflow.WithActivityOptions(ctx, opts)

    var result string
    return workflow.ExecuteActivity(ctx, MyActivity, input).Get(ctx, &result)
}
```

## Loading from config

`TemporalConfig` is designed to be populated from a YAML file via Viper:

```yaml
temporal:
  host: localhost
  port: 7233
  namespace: default
  task_queue: my-service
```

```go
var cfg temporal.TemporalConfig
if err := viper.UnmarshalKey("temporal", &cfg); err != nil {
    log.Fatal(err)
}

c, err := temporal.NewClient(cfg.Namespace, cfg.Host, cfg.Port)
```

## Default activity options

| Field | Default |
|---|---|
| `StartToCloseTimeout` | 1h |
| `RetryPolicy.InitialInterval` | 5s |
| `RetryPolicy.MaximumInterval` | 1m |
| `RetryPolicy.BackoffCoefficient` | 2.0 |
| `RetryPolicy.MaximumAttempts` | 3 |

## Errors

| Sentinel | Cause |
|---|---|
| `ErrEmptyHost` | `host` argument is empty |
| `ErrEmptyNamespace` | `namespace` argument is empty |

## Notes

- `NewClient` returns a `client.Client` from the Temporal SDK; the caller is responsible for calling `Close()` when done
- `DefaultActivityOpts` does not set `ScheduleToCloseTimeout`; use `WithScheduleToCloseTimeout` to add an end-to-end deadline
- passing `WithMaximumAttempts(0)` enables unlimited retries per the Temporal SDK convention
- `DefaultActivityOpts` is safe to call from workflow code; it allocates a new `RetryPolicy` on every call
