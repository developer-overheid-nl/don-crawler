package logging

import (
	"encoding/json"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFormatterNormalizesLevelsToLoggingContract(t *testing.T) {
	tests := []struct {
		name  string
		level log.Level
		want  string
	}{
		{name: "panic", level: log.PanicLevel, want: "ERROR"},
		{name: "fatal", level: log.FatalLevel, want: "ERROR"},
		{name: "error", level: log.ErrorLevel, want: "ERROR"},
		{name: "warning", level: log.WarnLevel, want: "WARN"},
		{name: "info", level: log.InfoLevel, want: "INFO"},
		{name: "debug", level: log.DebugLevel, want: "DEBUG"},
		{name: "trace", level: log.TraceLevel, want: "DEBUG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &log.Entry{
				Data:    log.Fields{"component": "test", "operation": "format"},
				Time:    time.Date(2026, time.August, 20, 8, 15, 30, 0, time.UTC),
				Level:   tt.level,
				Message: "level check",
			}

			encoded, err := (JSONFormatter{}).Format(entry)
			require.NoError(t, err)

			var event map[string]any
			require.NoError(t, json.Unmarshal(encoded, &event))
			assert.Equal(t, tt.want, event["level"])
		})
	}
}
