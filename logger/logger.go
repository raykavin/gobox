package logger

import (
	"fmt"
)

// Formatting consts
const (
	maxMessageSize = 65
	maxFileSize    = 25
	maxLineSize    = 4
)

type Logger struct {
	*Zerolog
}

// Print implements Logging.
func (s *Logger) Print(args ...any) {
	s.Logger.Print(args...)
}

// Debug implements Logging.
func (s *Logger) Debug(args ...any) {
	s.Logger.Debug().Msg(fmt.Sprint(args...))
}

// Info implements Logging.
func (s *Logger) Info(args ...any) {
	s.Logger.Info().Msg(fmt.Sprint(args...))
}

// Warn implements Logging.
func (s *Logger) Warn(args ...any) {
	s.Logger.Warn().Msg(fmt.Sprint(args...))
}

// Error implements Logging.
func (s *Logger) Error(args ...any) {
	s.Logger.Error().Msg(fmt.Sprint(args...))
}

// Fatal implements Logging.
func (s *Logger) Fatal(args ...any) {
	s.Logger.Fatal().Msg(fmt.Sprint(args...))
}

// Panic implements Logging.
func (s *Logger) Panic(args ...any) {
	s.Logger.Panic().Msg(fmt.Sprint(args...))
}

// Printf implements Logging.
func (s *Logger) Printf(format string, args ...any) {
	s.Logger.Printf(format, args...)
}

// Debugf implements Logging.
func (s *Logger) Debugf(format string, args ...any) {
	s.Logger.Debug().Msgf(format, args...)
}

// Infof implements Logging.
func (s *Logger) Infof(format string, args ...any) {
	s.Logger.Info().Msgf(format, args...)
}

// Warnf implements Logging.
func (s *Logger) Warnf(format string, args ...any) {
	s.Logger.Warn().Msgf(format, args...)
}

// Errorf implements Logging.
func (s *Logger) Errorf(format string, args ...any) {
	s.Logger.Error().Msgf(format, args...)
}

// Fatalf implements Logging.
func (s *Logger) Fatalf(format string, args ...any) {
	s.Logger.Fatal().Msgf(format, args...)
}

// Panicf implements Logging.
func (s *Logger) Panicf(format string, args ...any) {
	s.Logger.Panic().Msgf(format, args...)
}

// WithError implements Logging.
func (s *Logger) WithError(err error) *Logger {
	newLogger := s.With().Err(err).Logger()
	return &Logger{&Zerolog{Logger: &newLogger}}
}

// WithField implements Logging.
func (s *Logger) WithField(key string, value any) *Logger {
	newLogger := s.With().Interface(key, value).Logger()
	return &Logger{&Zerolog{Logger: &newLogger}}
}

// WithFields implements Logging.
func (s *Logger) WithFields(fields map[string]any) *Logger {
	newLogger := s.With().Fields(fields).Logger()
	return &Logger{&Zerolog{Logger: &newLogger}}
}
