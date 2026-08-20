package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

const appName = "oss-register"

// Event creates a log entry with the stable fields used for Loki filtering.
func Event(component, operation string) *log.Entry {
	return log.WithFields(log.Fields{
		"component": component,
		"operation": operation,
	})
}

// JSONFormatter writes one Loki-compatible JSON object per log entry.
type JSONFormatter struct{}

// Format implements logrus.Formatter.
func (JSONFormatter) Format(entry *log.Entry) ([]byte, error) {
	fields := make(log.Fields, len(entry.Data)+6)
	for key, value := range entry.Data {
		if err, ok := value.(error); ok {
			fields[key] = err.Error()
		} else {
			fields[key] = value
		}
	}

	fixedFields := log.Fields{
		"time":  entry.Time.UTC().Format(time.RFC3339Nano),
		"level": normalizedLevel(entry.Level),
		"msg":   entry.Message,
		"app":   appName,
	}
	for key, value := range fixedFields {
		fields[key] = value
	}

	if _, ok := fields["component"]; !ok {
		fields["component"] = "application"
	}

	if _, ok := fields["operation"]; !ok {
		fields["operation"] = "log"
	}

	var output bytes.Buffer
	if err := json.NewEncoder(&output).Encode(fields); err != nil {
		return nil, fmt.Errorf("encode log entry: %w", err)
	}

	return output.Bytes(), nil
}

func normalizedLevel(level log.Level) string {
	switch level {
	case log.PanicLevel, log.FatalLevel, log.ErrorLevel:
		return "ERROR"
	case log.WarnLevel:
		return "WARN"
	case log.InfoLevel:
		return "INFO"
	case log.DebugLevel, log.TraceLevel:
		return "DEBUG"
	default:
		return "ERROR"
	}
}
