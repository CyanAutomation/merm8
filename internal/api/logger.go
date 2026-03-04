package api

import (
	"io"
	"log/slog"
	"os"
)

// Logger defines structured logging used by API components.
type Logger interface {
	Info(msg string, fields ...any)
	Warn(msg string, fields ...any)
	Error(msg string, fields ...any)
}

type slogLogger struct {
	inner *slog.Logger
}

func (l *slogLogger) Info(msg string, fields ...any) {
	l.inner.Info(msg, fields...)
}

func (l *slogLogger) Warn(msg string, fields ...any) {
	l.inner.Warn(msg, fields...)
}

func (l *slogLogger) Error(msg string, fields ...any) {
	l.inner.Error(msg, fields...)
}

// NewLogger returns a JSON structured logger with an optional component field.
func NewLogger(component string) Logger {
	return newJSONLogger(os.Stdout, component)
}

func newJSONLogger(w io.Writer, component string) Logger {
	logger := slog.New(slog.NewJSONHandler(w, nil))
	if component != "" {
		logger = logger.With("component", component)
	}
	return &slogLogger{inner: logger}
}

type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

func normalizeLogger(logger Logger) Logger {
	if logger == nil {
		return noopLogger{}
	}
	return logger
}
