package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preserveGlobalLogger(t *testing.T) {
	t.Helper()

	originalLogger := slog.Default()

	t.Cleanup(func() {
		slog.SetDefault(originalLogger)
		viper.Reset()
	})
}

func TestConfigureLoggingDefaultsToInfo(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()

	var console bytes.Buffer
	closer, err := configureLogging(&console)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	slog.Debug("hidden diagnostic", "component", "test", "operation", "emit")
	slog.Info("visible lifecycle event", "component", "test", "operation", "emit")

	assert.NotContains(t, console.String(), "hidden diagnostic")
	assert.Contains(t, console.String(), "visible lifecycle event")
}

func TestConfigureLoggingWritesStructuredApplicationContext(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()

	var console bytes.Buffer
	closer, err := configureLogging(&console)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	slog.Info(
		"Crawler run completed",
		"component", "crawler",
		"operation", "complete",
	)

	var event map[string]any
	require.NoError(t, json.Unmarshal(console.Bytes(), &event))
	assert.Equal(t, "INFO", event["level"])
	assert.Equal(t, "Crawler run completed", event["msg"])
	assert.Equal(t, "don-crawler", event["app"])
	assert.Equal(t, "crawler", event["component"])
	assert.Equal(t, "complete", event["operation"])

	timestamp, ok := event["time"].(string)
	require.True(t, ok)
	_, err = time.Parse(time.RFC3339Nano, timestamp)
	require.NoError(t, err)
}

func TestConfigureLoggingPreservesSuppliedContext(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()

	var console bytes.Buffer
	closer, err := configureLogging(&console)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	slog.Info(
		"startup diagnostic",
		"component", "application",
		"operation", "log",
	)

	var event map[string]any
	require.NoError(t, json.Unmarshal(console.Bytes(), &event))
	assert.Equal(t, "application", event["component"])
	assert.Equal(t, "log", event["operation"])
}

func TestConfigureLoggingSerializesErrorsAsStrings(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()

	var console bytes.Buffer
	closer, err := configureLogging(&console)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	slog.Error(
		"API request failed",
		"component", "api_client",
		"operation", "request",
		"error", errors.New("connection refused"),
	)

	var event map[string]any
	require.NoError(t, json.Unmarshal(console.Bytes(), &event))
	assert.Equal(t, "connection refused", event["error"])
}

func TestConfigureLoggingUsesConfiguredLevel(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()
	viper.Set("LOG_LEVEL", "debug")

	var console bytes.Buffer
	closer, err := configureLogging(&console)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	slog.Debug("configured diagnostic", "component", "test", "operation", "emit")

	assert.Contains(t, console.String(), "configured diagnostic")
}

func TestConfigureLoggingRejectsInvalidLevel(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()
	viper.Set("LOG_LEVEL", "verbose-ish")

	_, err := configureLogging(io.Discard)

	require.ErrorContains(t, err, "unsupported LOG_LEVEL")
}

func TestConfigureLoggingRejectsTraceLevel(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()
	viper.Set("LOG_LEVEL", "trace")

	_, err := configureLogging(io.Discard)

	require.ErrorContains(t, err, "unsupported LOG_LEVEL")
}

func TestConfigureLoggingWritesToConsoleAndOptionalFile(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()
	logPath := filepath.Join(t.TempDir(), "crawler.log")
	viper.Set("ENABLE_FILE_LOG", true)
	viper.Set("LOG_FILE", logPath)

	var console bytes.Buffer
	closer, err := configureLogging(&console)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	slog.Warn("degraded repository data", "component", "crawler", "operation", "scan_repository")

	fileContents, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, console.String(), "degraded repository data")
	assert.Contains(t, string(fileContents), "degraded repository data")
}
