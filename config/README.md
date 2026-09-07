# config

The `config` package provides configuration loading built on top of [Viper](https://github.com/spf13/viper). It is designed for shared service configuration where applications need a consistent way to load files, expand environment variables, validate data, and react to runtime changes.

## Import

```go
import "github.com/raykavin/gobox/config"
```

## What it provides

- support for the file formats Viper handles, such as `yaml`, `json`, and `toml`
- `${VAR}` environment variable expansion inside configuration files
- optional validation applied after loading
- thread-safe access to the current configuration
- change subscriptions over channels, plus callback hooks
- debounced reload handling for watched configuration files

## Main types

- `LoaderOptions[T]`: configures loader behavior; `DefaultLoaderOptions[T]()` returns a populated starting point
- `Loader[T]`: loads, stores, watches, and reloads configuration
- `ConfigChangeEvent[T]`: describes a reload, whether it succeeded or failed
- `ConfigWatcher[T]`: the `Subscribe`/`Unsubscribe` interface that `*Loader[T]` satisfies

## Example

```go
package main

import (
	"errors"
	"log"
	"time"

	"github.com/raykavin/gobox/config"
)

type AppConfig struct {
	AppName string `mapstructure:"app_name"`
	Port    int    `mapstructure:"port"`
	DBURL   string `mapstructure:"db_url"`
}

func main() {
	loader := config.NewViper[AppConfig](&config.LoaderOptions[AppConfig]{
		ConfigName:     "config",
		ConfigType:     "yaml",
		ConfigPaths:    []string{".", "./config"},
		WatchConfig:    true,
		ReloadDebounce: 2 * time.Second,
		OnConfigChange: func(cfg *AppConfig) {
			log.Printf("configuration reloaded for %s", cfg.AppName)
		},
		OnConfigChangeError: func(err error) {
			log.Printf("failed to reload configuration: %v", err)
		},
	})
	defer loader.Stop()

	cfg, err := loader.LoadWithValidation(func(cfg *AppConfig) error {
		if cfg.Port == 0 {
			return errors.New("port is required")
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("starting %s on port %d", cfg.AppName, cfg.Port)
}
```

## Example config file

```yaml
app_name: my-service
port: 8080
db_url: ${DATABASE_URL}
```

Expansion runs over the raw file text before parsing, so `${VAR}` works anywhere in the document, including in the middle of a DSN. Unset variables expand to the empty string, which is `os.ExpandEnv` behavior.

## Reacting to changes

Two mechanisms are available, and they can be used together:

```go
// Callbacks, set on LoaderOptions, fire synchronously on the watcher goroutine.
opts.OnConfigChange = func(cfg *AppConfig) { /* ... */ }
opts.OnConfigChangeError = func(err error) { /* ... */ }

// Channels, for consumers that want to select on config changes.
events := loader.Subscribe()
defer loader.Unsubscribe(events)

for ev := range events {
	if ev.Error != nil {
		log.Printf("reload failed at %s: %v", ev.Timestamp, ev.Error)
		continue
	}
	log.Printf("reloaded: %+v", ev.NewConfig)
}
```

## Reference

### LoaderOptions

| Field | Default from `DefaultLoaderOptions` | Description |
|---|---|---|
| `ConfigName` | `"config"` | File name without extension |
| `ConfigType` | `"yaml"` | File format |
| `ConfigPaths` | `[".", "./config", "/etc/app", "$HOME/.app"]` | Search paths, in order |
| `WatchConfig` | `false` | Enables file watching and reload |
| `ReloadDebounce` | `1s` | Window in which repeated write events collapse into one reload |
| `OnConfigChange` | `nil` | Called with the new config after a successful reload |
| `OnConfigChangeError` | `nil` | Called with the error after a failed reload |

### Loader methods

| Method | Description |
|---|---|
| `Load() (*T, error)` | Reads and stores the configuration, and starts watching when `WatchConfig` is set |
| `LoadWithValidation(func(*T) error) (*T, error)` | `Load`, then applies the validator, and retains it for later reloads |
| `GetCurrent() *T` | The most recently loaded configuration, safe for concurrent use |
| `Reload() error` | Re-reads the file on demand and notifies subscribers |
| `Subscribe() <-chan ConfigChangeEvent[T]` | A new buffered channel of reload events |
| `Unsubscribe(<-chan ConfigChangeEvent[T])` | Removes a subscriber |
| `Stop()` | Stops watching and closes every subscriber channel |
| `GetViper() *viper.Viper` | The underlying Viper instance, for settings this wrapper does not expose |

### ConfigChangeEvent

| Field | Description |
|---|---|
| `OldConfig` | Configuration in effect before the reload |
| `NewConfig` | Configuration after the reload; `nil` when the reload failed |
| `Error` | Non-nil when the reload failed |
| `Timestamp` | When the event was produced |

### Errors

| Sentinel | Cause |
|---|---|
| `ErrConfigFileReadFailed` | The file could not be read, or the expanded text could not be parsed |
| `ErrConfigUnmarshalFailed` | The document does not unmarshal into `T` |
| `ErrConfigValidationFailed` | The validator passed to `LoadWithValidation` returned an error |
| `ErrConfigReloadFailed` | A reload triggered by the watcher failed |
| `ErrValidationReloadFailed` | `ErrConfigReloadFailed` joined with `ErrConfigValidationFailed` |
| `ErrConfigWatchUnavailable` | `WatchConfig` is set but no config file could be resolved to watch |

All are joined with the underlying cause, so `errors.Is` matches the sentinel and `errors.Unwrap` reaches the detail.

## Notes

- subscriber channels are buffered to 10 events and sends are non-blocking, so a slow consumer drops events rather than stalling the watcher; treat an event as a signal to call `GetCurrent()`, not as a guaranteed log of every change
- `Load` returns `ErrConfigWatchUnavailable` rather than silently succeeding when watching is requested but no file was found. Viper's own `WatchConfig` fails silently in that case, which would leave a caller convinced its configuration is being watched
- `Stop` closes subscriber channels, so a `range` over a subscription terminates; do not call `Unsubscribe` on a channel after `Stop`
- `LoadWithValidation` records the validator for later reloads, so every subsequent `Reload()` is validated too
- validation is ordered differently on the two paths. `Reload()` validates before storing, so a failing reload leaves the previous configuration in place. `LoadWithValidation` stores first and validates after, so on the initial load a config that fails its validator is still visible to `GetCurrent()` even though the call returns an error; treat that error as fatal rather than continuing to serve from the loader
