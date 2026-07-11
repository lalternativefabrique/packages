// Package logger defines a small structured-logging interface and a
// default slog-backed implementation.
//
// The interface lets the rest of the boilerplate stay logger-agnostic
// while consumers plug in any slog handler (JSON, text, OTEL bridge...).
package logger

import (
	"context"
	"log/slog"
	"os"
)

// Logger is the structured-logging interface used across the boilerplate.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
}

// Field is a single key/value annotation on a log record.
type Field struct {
	Key   string
	Value any
}

// NewField constructs a Field.
func NewField(key string, value any) Field { return Field{Key: key, Value: value} }

// Typed helpers for the most common field kinds.
func String(key, value string) Field { return Field{Key: key, Value: value} }
func Int(key string, value int) Field { return Field{Key: key, Value: value} }
func Int64(key string, value int64) Field { return Field{Key: key, Value: value} }
func Bool(key string, value bool) Field { return Field{Key: key, Value: value} }
func Any(key string, value any) Field { return Field{Key: key, Value: value} }

// SlogLogger adapts a *slog.Logger to the Logger interface so consumers
// can plug in any slog handler (JSON, text, OTEL bridge, ...).
type SlogLogger struct {
	l *slog.Logger
}

// NewSlogLogger wraps an existing *slog.Logger. Pass slog.Default() for the
// process-wide default, or build a custom one.
func NewSlogLogger(l *slog.Logger) *SlogLogger {
	if l == nil {
		l = slog.Default()
	}
	return &SlogLogger{l: l}
}

// NewJSONSlogLogger returns a JSON slog logger writing to stdout at the
// given level.
func NewJSONSlogLogger(level slog.Level) *SlogLogger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return &SlogLogger{l: slog.New(h)}
}

// NewTextSlogLogger returns a text slog logger writing to stdout at the
// given level.
func NewTextSlogLogger(level slog.Level) *SlogLogger {
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return &SlogLogger{l: slog.New(h)}
}

// Slog returns the underlying *slog.Logger for direct use.
func (s *SlogLogger) Slog() *slog.Logger { return s.l }

func (s *SlogLogger) Debug(msg string, fields ...Field) { s.log(slog.LevelDebug, msg, fields) }
func (s *SlogLogger) Info(msg string, fields ...Field)  { s.log(slog.LevelInfo, msg, fields) }
func (s *SlogLogger) Warn(msg string, fields ...Field)  { s.log(slog.LevelWarn, msg, fields) }
func (s *SlogLogger) Error(msg string, fields ...Field) { s.log(slog.LevelError, msg, fields) }

// Fatal logs at error level then exits the process.
func (s *SlogLogger) Fatal(msg string, fields ...Field) {
	s.log(slog.LevelError, msg, fields)
	os.Exit(1)
}

func (s *SlogLogger) log(lvl slog.Level, msg string, fields []Field) {
	if !s.l.Enabled(context.Background(), lvl) {
		return
	}
	attrs := make([]slog.Attr, 0, len(fields))
	for _, f := range fields {
		attrs = append(attrs, slog.Any(f.Key, f.Value))
	}
	s.l.LogAttrs(context.Background(), lvl, msg, attrs...)
}

// Nop is a Logger that discards all input. Useful as a default in tests.
type Nop struct{}

// NewNop returns a Logger that discards all messages.
func NewNop() *Nop { return &Nop{} }

func (Nop) Debug(string, ...Field) {}
func (Nop) Info(string, ...Field)  {}
func (Nop) Warn(string, ...Field)  {}
func (Nop) Error(string, ...Field) {}
func (Nop) Fatal(string, ...Field) {}
