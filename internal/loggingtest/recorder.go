package loggingtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	commonlogging "github.com/developer-overheid-nl/don-register-common/logging"
)

// Recorder captures application logs as decoded JSON events for tests.
type Recorder struct {
	output bytes.Buffer
}

// Capture installs a shared JSON logger until the test completes.
func Capture(tb testing.TB, level string) *Recorder {
	tb.Helper()

	recorder := &Recorder{}

	logger, err := commonlogging.NewJSONLogger(&recorder.output, "oss-register", level)
	if err != nil {
		tb.Fatalf("create test logger: %v", err)
	}

	previousLogger := slog.Default()

	slog.SetDefault(logger)
	tb.Cleanup(func() { slog.SetDefault(previousLogger) })

	return recorder
}

// Events decodes every newline-delimited JSON object captured so far.
func (recorder *Recorder) Events(tb testing.TB) []map[string]any {
	tb.Helper()

	decoder := json.NewDecoder(bytes.NewReader(recorder.output.Bytes()))
	events := make([]map[string]any, 0)

	for {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			tb.Fatalf("decode captured log event: %v", err)
		}

		events = append(events, event)
	}

	return events
}
