package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var (
	ErrConfigFileReadFailed   = errors.New("error reading configuration file")
	ErrConfigUnmarshalFailed  = errors.New("error parsing configuration")
	ErrConfigValidationFailed = errors.New("invalid configuration")
	ErrConfigReloadFailed     = errors.New("failed reloading the configuration")
	ErrValidationReloadFailed = errors.Join(ErrConfigReloadFailed, ErrConfigValidationFailed)
)

// LoaderOptions defines configuration loader options
type LoaderOptions[T any] struct {
	ConfigName          string        // Configuration file name without extension
	ConfigType          string        // Configuration file type (yaml, json, toml, etc.)
	ConfigPaths         []string      // Paths where the configuration file is searched
	WatchConfig         bool          // Enables file watching for reload
	ReloadDebounce      time.Duration // Debounce duration for reload events
	OnConfigChange      func(*T)      // Callback executed on successful reload
	OnConfigChangeError func(error)   // Callback executed on reload error
}

// ConfigChangeEvent represents a configuration change event
type ConfigChangeEvent[T any] struct {
	OldConfig *T        // Previous configuration
	NewConfig *T        // New configuration
	Error     error     // Error if reload failed
	Timestamp time.Time // Time of the event
}

// ConfigWatcher defines subscription behavior for config changes
type ConfigWatcher[T any] interface {
	Subscribe() <-chan ConfigChangeEvent[T]
	Unsubscribe(<-chan ConfigChangeEvent[T])
}

// DefaultLoaderOptions returns default configuration loader options
func DefaultLoaderOptions[T any]() *LoaderOptions[T] {
	return &LoaderOptions[T]{
		ConfigName:          "config",
		ConfigType:          "yaml",
		ConfigPaths:         []string{".", "./config", "/etc/app", "$HOME/.app"},
		WatchConfig:         false,
		ReloadDebounce:      1 * time.Second,
		OnConfigChange:      nil,
		OnConfigChangeError: nil,
	}
}

// Loader handles configuration loading and watching
type Loader[T any] struct {
	options     *LoaderOptions[T]           // Loader configuration options
	viper       *viper.Viper                // Viper instance
	current     *T                          // Current configuration
	mutex       sync.RWMutex                // Protects current config
	subscribers []chan ConfigChangeEvent[T] // Subscribers to config changes
	subMutex    sync.RWMutex                // Protects subscribers list
	validator   func(*T) error              // Optional validation function
	ctx         context.Context             // Context for lifecycle control
	cancel      context.CancelFunc          // Cancels context
	debouncer   *time.Timer                 // Debounce timer for reload
}

// NewViper creates a new configuration loader
func NewViper[T any](options *LoaderOptions[T]) *Loader[T] {
	if options == nil {
		options = DefaultLoaderOptions[T]()
	}

	v := viper.New()

	// Sets config file name and type
	v.SetConfigName(options.ConfigName)
	v.SetConfigType(options.ConfigType)

	// Adds config search paths
	for _, path := range options.ConfigPaths {
		v.AddConfigPath(path)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Loader[T]{
		options:     options,
		viper:       v,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make([]chan ConfigChangeEvent[T], 0),
	}
}

// GetViper returns the underlying Viper instance
func (cl *Loader[T]) GetViper() *viper.Viper {
	return cl.viper
}

// Load loads configuration from file and expands environment variables
func (cl *Loader[T]) Load() (*T, error) {
	config, err := cl.loadConfig()
	if err != nil {
		return nil, err
	}

	cl.mutex.Lock()
	cl.current = config
	cl.mutex.Unlock()

	// Starts file watching if enabled
	if cl.options.WatchConfig {
		cl.startWatching()
	}

	return config, nil
}

// LoadWithValidation loads configuration and applies validation
func (cl *Loader[T]) LoadWithValidation(validator func(*T) error) (*T, error) {
	cl.validator = validator

	config, err := cl.Load()
	if err != nil {
		return nil, err
	}

	if validator != nil {
		if err := validator(config); err != nil {
			return nil, errors.Join(ErrConfigValidationFailed, err)
		}
	}

	return config, nil
}

// GetCurrent returns the current configuration safely
func (cl *Loader[T]) GetCurrent() *T {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()
	return cl.current
}

// Subscribe registers a new subscriber for config changes
func (cl *Loader[T]) Subscribe() <-chan ConfigChangeEvent[T] {
	cl.subMutex.Lock()
	defer cl.subMutex.Unlock()

	ch := make(chan ConfigChangeEvent[T], 10)
	cl.subscribers = append(cl.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber
func (cl *Loader[T]) Unsubscribe(ch <-chan ConfigChangeEvent[T]) {
	cl.subMutex.Lock()
	defer cl.subMutex.Unlock()

	for i, subscriber := range cl.subscribers {
		if subscriber == ch {
			close(subscriber)
			cl.subscribers = append(cl.subscribers[:i], cl.subscribers[i+1:]...)
			break
		}
	}
}

// Reload reloads configuration from file
func (cl *Loader[T]) Reload() error {
	oldConfig := cl.GetCurrent()

	newConfig, err := cl.loadConfig()
	if err != nil {
		cl.notifyError(err)
		return errors.Join(ErrConfigReloadFailed, err)
	}

	// Validates new configuration if validator is defined
	if cl.validator != nil {
		if err := cl.validator(newConfig); err != nil {
			validationErr := errors.Join(ErrValidationReloadFailed, err)
			cl.notifyError(validationErr)
			return validationErr
		}
	}

	cl.mutex.Lock()
	cl.current = newConfig
	cl.mutex.Unlock()

	cl.notifyChange(oldConfig, newConfig)
	return nil
}

// Stop stops the loader and releases resources
func (cl *Loader[T]) Stop() {
	cl.cancel()

	if cl.debouncer != nil {
		cl.debouncer.Stop()
	}

	cl.subMutex.Lock()
	defer cl.subMutex.Unlock()

	for _, subscriber := range cl.subscribers {
		close(subscriber)
	}
	cl.subscribers = nil
}

// loadConfig reads the config file, expands ${ENV} variables, and unmarshals into struct
func (cl *Loader[T]) loadConfig() (*T, error) {
	var config T

	// Reads configuration file
	if err := cl.viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, errors.Join(ErrConfigFileReadFailed, err)
		}
	}

	configFile := cl.viper.ConfigFileUsed()
	if configFile != "" {
		// Reads raw file content
		content, err := os.ReadFile(configFile)
		if err != nil {
			return nil, errors.Join(ErrConfigFileReadFailed, err)
		}

		// Expands from environment
		expanded := os.ExpandEnv(string(content))

		// Creates a new Viper instance for expanded content
		newViper := viper.New()
		newViper.SetConfigType(cl.options.ConfigType)

		if err := newViper.ReadConfig(bytes.NewBuffer([]byte(expanded))); err != nil {
			return nil, errors.Join(ErrConfigFileReadFailed, err)
		}

		cl.viper = newViper
	}

	// Unmarshal's configuration into struct
	if err := cl.viper.Unmarshal(&config); err != nil {
		return nil, errors.Join(ErrConfigUnmarshalFailed, err)
	}

	return &config, nil
}

// startWatching enables file watching for config changes
func (cl *Loader[T]) startWatching() {
	cl.viper.WatchConfig()
	cl.viper.OnConfigChange(func(_ fsnotify.Event) {
		cl.debouncedReload()
	})
}

// debouncedReload debounces reload events
func (cl *Loader[T]) debouncedReload() {
	if cl.debouncer != nil {
		cl.debouncer.Stop()
	}

	cl.debouncer = time.AfterFunc(cl.options.ReloadDebounce, func() {
		_ = cl.Reload()
	})
}

// notifyChange notifies subscribers of successful reload
func (cl *Loader[T]) notifyChange(oldConfig, newConfig *T) {
	event := ConfigChangeEvent[T]{
		OldConfig: oldConfig,
		NewConfig: newConfig,
		Timestamp: time.Now(),
	}

	if cl.options.OnConfigChange != nil {
		go cl.options.OnConfigChange(newConfig)
	}

	cl.subMutex.RLock()
	defer cl.subMutex.RUnlock()

	for _, subscriber := range cl.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

// notifyError notifies subscribers of reload errors
func (cl *Loader[T]) notifyError(err error) {
	event := ConfigChangeEvent[T]{
		Error:     err,
		Timestamp: time.Now(),
	}

	if cl.options.OnConfigChangeError != nil {
		go cl.options.OnConfigChangeError(err)
	}

	cl.subMutex.RLock()
	defer cl.subMutex.RUnlock()

	for _, subscriber := range cl.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}
