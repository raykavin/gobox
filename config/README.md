# config

The `config` package provides configuration loading built on top of Viper. It is designed for shared service configuration where applications need a consistent way to load files, expand environment variables, validate data, and react to runtime changes.

## Import

```go
import "github.com/raykavin/gokit/config"
```

## What it provides

- support for file formats handled by Viper such as `yaml`, `json`, and `toml`
- environment variable expansion inside configuration files
- optional validation after loading
- current config access through a thread-safe loader
- change subscriptions and callbacks
- debounced reload handling for watched config files

## Main types

- `LoaderOptions[T]`: configures the loader behavior
- `Loader[T]`: loads, stores, watches, and reloads configuration
- `ConfigChangeEvent[T]`: describes successful reloads or reload failures

## Example

```go
package main

import (
	"errors"
	"log"
	"time"

	"github.com/raykavin/gokit/config"
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

## Notes

- `Load()` reads and stores the current config instance
- `LoadWithValidation()` applies a validation function after loading
- `GetCurrent()` returns the latest loaded config safely
- `Reload()` refreshes the config from disk
- `Subscribe()` and `Unsubscribe()` let consumers react to config changes
- `Stop()` stops watchers and closes subscriber channels
