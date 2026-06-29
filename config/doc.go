// Package config provides configuration loading built on top of Viper.
//
// It supports yaml, json, toml, and any other format Viper understands.
// Environment variables are expanded automatically inside configuration files,
// and an optional validator can reject invalid configurations at load time.
//
// # Basic loading
//
//	loader := config.NewViper[AppConfig](&config.LoaderOptions[AppConfig]{
//	    ConfigName:  "config",
//	    ConfigType:  "yaml",
//	    ConfigPaths: []string{".", "./config"},
//	})
//
//	cfg, err := loader.Load()
//
// # Loading with validation
//
//	cfg, err := loader.LoadWithValidation(func(c *AppConfig) error {
//	    if c.Port == 0 {
//	        return errors.New("port is required")
//	    }
//	    return nil
//	})
//
// # Watching for changes
//
// Set WatchConfig: true to start a file watcher. Use Subscribe to receive
// typed change events, or OnConfigChange for a simpler callback.
//
//	ch := loader.Subscribe()
//	defer loader.Unsubscribe(ch)
//
//	for event := range ch {
//	    if event.Error != nil {
//	        log.Printf("reload failed: %v", event.Error)
//	        continue
//	    }
//	    log.Printf("config reloaded: port %d", event.NewConfig.Port)
//	}
package config
