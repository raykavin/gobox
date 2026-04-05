# gokit

`gokit` is a Go module for shared libraries that can be reused across different projects. The goal is to keep common building blocks in one place so teams can reduce code duplication, standardize recurring infrastructure concerns, and move faster when starting or evolving services.

At the moment, the repository provides utilities for configuration loading, logging, and SQL database access, with an emphasis on low coupling and practical reuse between applications.

## Purpose

This module exists to:

- centralize reusable code across projects
- avoid reimplementing the same utilities in multiple repositories
- provide a common base for infrastructure-related concerns
- improve maintenance, consistency, and long-term reuse across services

## Available packages

### `config`

The `config` package provides configuration loading based on Viper, with support for:

- reading `yaml`, `json`, `toml`, and other formats supported by Viper
- expanding environment variables such as `${DB_HOST}` inside config files
- optional validation after loading
- runtime configuration reloads
- callbacks and subscriptions for config change events
- debounce support to avoid excessive reloads during file updates

Import:

```go
import "github.com/raykavin/gokit/config"
```

Example:

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

Example configuration using environment variables:

```yaml
app_name: my-service
port: 8080
db_url: ${DATABASE_URL}
```

### `database`

The `database` package provides a lightweight wrapper around `database/sql` while keeping the module free from driver-specific dependencies. The consuming project chooses the driver and defines how each row should be mapped into the target type.

Import:

```go
import "github.com/raykavin/gokit/database"
```

Example:

```go
package main

import (
	"context"
	"database/sql"
	"log"

	_ "<your-driver>"

	"github.com/raykavin/gokit/database"
)

type User struct {
	ID   int
	Name string
}

func main() {
	conn, err := database.New(database.Config{
		Driver: "postgres",
		DSN:    "postgres://user:pass@localhost:5432/app?sslmode=disable",
	}, func(rows *sql.Rows) (User, error) {
		var user User
		err := rows.Scan(&user.ID, &user.Name)
		return user, err
	})
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	users, err := conn.Query(context.Background(), "select id, name from users")
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("users found: %d", len(users))
}
```

### `logger`

The `logger` package provides a reusable logging layer built on top of `zerolog`, with support for:

- configurable log level and timestamp layout
- colored console formatting
- contextual fields for structured logging
- helper methods for success, failure, benchmark, and API request logs
- caller metadata in log output
- error rendering with improved readability for nested error details

Import:

```go
import "github.com/raykavin/gokit/logger"
```

Example:

```go
package main

import (
	"log"
	"time"

	"github.com/raykavin/gokit/logger"
)

func main() {
	appLogger, err := logger.New(&logger.Config{
		Level:          "debug",
		DateTimeLayout: time.RFC3339,
		Colored:        true,
		JSONFormat:     false,
		UseEmoji:       false,
	})
	if err != nil {
		log.Fatal(err)
	}

	appLogger.Info().
		Str("service", "billing").
		Msg("service started")

	appLogger.WithContext(map[string]any{
		"request_id": "req-123",
		"component":  "http",
	}).API("GET", "/health", "127.0.0.1", 200, 42*time.Millisecond)
}
```

## Installation

To use the module in another project:

```bash
go get github.com/raykavin/gokit
```

Then import only the packages you need in the consuming application.

## Reuse strategy

This repository can grow over time as new shared libraries emerge from real project needs. A good rule of thumb is to move code here when it:

- appears repeatedly across different services
- represents generic infrastructure or integration logic
- is not tightly coupled to the business rules of a single system

That way, each application can stay focused on domain logic while reusing a common foundation for recurring technical concerns.
