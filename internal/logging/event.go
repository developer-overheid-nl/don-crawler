package logging

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
)

// Entry adds stable crawler context to events emitted by the shared logger.
type Entry struct {
	logger *slog.Logger
}

// Event creates a log entry with the stable fields used for Loki filtering.
func Event(component, operation string) *Entry {
	return &Entry{logger: slog.Default().With(
		"component", component,
		"operation", operation,
	)}
}

func (entry *Entry) WithField(key string, value any) *Entry {
	return &Entry{logger: entry.logger.With(key, value)}
}

func (entry *Entry) WithFields(fields map[string]any) *Entry {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	attributes := make([]any, 0, len(fields)*2)
	for _, key := range keys {
		attributes = append(attributes, key, fields[key])
	}

	return &Entry{logger: entry.logger.With(attributes...)}
}

func (entry *Entry) WithError(err error) *Entry {
	return entry.WithField(FieldError, err)
}

func (entry *Entry) Debug(message string) {
	entry.logger.Debug(message)
}

func (entry *Entry) Debugf(format string, args ...any) {
	entry.Debug(fmt.Sprintf(format, args...))
}

func (entry *Entry) Info(message string) {
	entry.logger.Info(message)
}

func (entry *Entry) Infof(format string, args ...any) {
	entry.Info(fmt.Sprintf(format, args...))
}

func (entry *Entry) Warn(message string) {
	entry.logger.Warn(message)
}

func (entry *Entry) Warnf(format string, args ...any) {
	entry.Warn(fmt.Sprintf(format, args...))
}

func (entry *Entry) Error(message string) {
	entry.logger.Error(message)
}

func (entry *Entry) Errorf(format string, args ...any) {
	entry.Error(fmt.Sprintf(format, args...))
}

func (entry *Entry) Fatal(message string) {
	entry.Error(message)
	os.Exit(1)
}
