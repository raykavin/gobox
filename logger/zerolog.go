package logger

import (
	"errors"
	"time"

	"github.com/fatih/color"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

const logIfTag = "Infra: Log"

var ErrInvalidLogLevel = errors.New("invalid logger level")

// Config holds logger configuration
type Config struct {
	Level          string
	DateTimeLayout string
	Colored        bool
	JSONFormat     bool
	UseEmoji       bool
}

// logLevel represents a log level with its properties
type logLevel struct {
	Text  string
	Color *color.Color
}

// Zerolog wraps zerolog.Logger with enhanced functionality
type Zerolog struct {
	*zerolog.Logger
	config *Config
}

// Log levels definitions
var logLevels = map[string]logLevel{
	zerolog.LevelTraceValue: {
		Text:  "TRAC",
		Color: color.New(color.FgHiBlack, color.Bold),
	},
	zerolog.LevelDebugValue: {
		Text:  "DEBG",
		Color: color.New(color.FgHiBlue, color.Bold),
	},
	zerolog.LevelInfoValue: {
		Text:  "INFO",
		Color: color.New(color.FgHiGreen, color.Bold),
	},
	zerolog.LevelWarnValue: {
		Text:  "WARN",
		Color: color.New(color.FgHiYellow, color.Bold),
	},
	zerolog.LevelErrorValue: {
		Text:  "ERRO",
		Color: color.New(color.FgHiRed, color.Bold),
	},
	zerolog.LevelFatalValue: {
		Text:  "FATL",
		Color: color.New(color.FgHiRed, color.Bold),
	},
	zerolog.LevelPanicValue: {
		Text:  "PANC",
		Color: color.New(color.FgWhite, color.BgRed, color.Bold, color.BlinkSlow),
	},
}

// New creates a new Logger instance
func New(config *Config) (*Zerolog, error) {
	if config == nil {
		config = &Config{
			Level:          "info",
			DateTimeLayout: time.RFC3339,
			Colored:        true,
			JSONFormat:     false,
			UseEmoji:       false,
		}
	}

	// Setup error stack marshaler
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	// Parse log level
	logMode, err := zerolog.ParseLevel(config.Level)
	if err != nil {
		return nil, errors.Join(ErrInvalidLogLevel, err)
	}
	zerolog.SetGlobalLevel(logMode)

	// Create logger based on format
	var logger zerolog.Logger
	if config.JSONFormat {
		logger = createJSONLogger(config)
	} else {
		logger = createConsoleLogger(config)
	}

	// Add caller information
	logger = logger.With().CallerWithSkipFrameCount(3).Logger()

	return &Zerolog{
		Logger: &logger,
		config: config,
	}, nil
}

// Success logs a success message
func (zl *Zerolog) Success(msg string) {
	zl.Info().Msg(msg)
}

// Failure logs a failure message
func (zl *Zerolog) Failure(msg string) {
	zl.Error().Msg(msg)
}

// Benchmark logs a benchmark result
func (zl *Zerolog) Benchmark(name string, duration time.Duration) {
	msg := "Benchmark:"

	zl.Debug().
		Str("duracao", duration.String()).
		Msgf("%s %s", msg, name)
}

// API logs an API request
func (zl *Zerolog) API(method, path, remoteAddr string, statusCode int, duration time.Duration, skipFrameCount ...int) {
	// Basic validation of required parameters.
	if method == "" || path == "" {
		zl.Logger.Error().
			Str("metodo", method).
			Str("caminho", path).
			Msg("parâmetros inválidos para log API")
		return
	}

	// Configure the base logger.
	logger := zl.Logger
	if len(skipFrameCount) > 0 && skipFrameCount[0] > 0 {
		newLogger := logger.With().CallerWithSkipFrameCount(skipFrameCount[0]).Logger()
		logger = &newLogger
	}

	// Prepare the message
	message := "Requisição API"
	formattedDuration := duration.Round(time.Millisecond).String()

	logger.WithLevel(zl.getStatusLevel(statusCode)).
		Str("metodo", method).
		Str("caminho", path).
		Str("endereco_remoto", remoteAddr).
		Int("status", statusCode).
		Str("duracao", formattedDuration).
		Msg(message)
}

// WithContext creates a new logger with additional context
func (zl *Zerolog) WithContext(ctx map[string]any) *Zerolog {
	event := zl.With()
	for k, v := range ctx {
		event = event.Interface(k, v)
	}
	logger := event.Logger()
	return &Zerolog{
		Logger: &logger,
		config: zl.config,
	}
}

// getStatusLevel returns the appropriate log level for HTTP status code
func (*Zerolog) getStatusLevel(statusCode int) zerolog.Level {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return zerolog.InfoLevel
	case statusCode >= 300 && statusCode < 400:
		return zerolog.InfoLevel
	case statusCode >= 400 && statusCode < 500:
		return zerolog.WarnLevel
	case statusCode >= 500:
		return zerolog.ErrorLevel
	default:
	}
	return zerolog.InfoLevel
}
