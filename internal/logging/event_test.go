package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	commonlogging "github.com/developer-overheid-nl/don-register-common/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventAddsStructuredCrawlerContext(t *testing.T) {
	output := captureEvents(t)

	Event("crawler", "complete").
		WithField(FieldRepository, "owner/repository").
		Info("Crawler run completed")

	event := decodeEvent(t, output)
	assert.Equal(t, "INFO", event["level"])
	assert.Equal(t, "Crawler run completed", event["msg"])
	assert.Equal(t, "don-crawler", event["app"])
	assert.Equal(t, "crawler", event["component"])
	assert.Equal(t, "complete", event["operation"])
	assert.Equal(t, "owner/repository", event[FieldRepository])
}

func TestEventPreservesFormattedWarningsAndErrors(t *testing.T) {
	output := captureEvents(t)

	Event("crawler", "check_publiccode").
		WithFields(map[string]any{FieldStatusCode: 502}).
		Warnf("publiccode.yml returned status %d", 502)

	event := decodeEvent(t, output)
	assert.Equal(t, "WARN", event["level"])
	assert.Equal(t, "publiccode.yml returned status 502", event["msg"])
	assert.Equal(t, float64(502), event[FieldStatusCode])
}

func captureEvents(t *testing.T) *bytes.Buffer {
	t.Helper()

	var output bytes.Buffer
	logger, err := commonlogging.NewJSONLogger(&output, "don-crawler", "debug")
	require.NoError(t, err)

	previousLogger := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	return &output
}

func decodeEvent(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()

	var event map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &event))

	return event
}
