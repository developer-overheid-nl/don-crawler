package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preserveGlobalLogger(t *testing.T) {
	t.Helper()

	logger := log.StandardLogger()
	originalLevel := logger.GetLevel()
	originalOutput := logger.Out
	originalFormatter := logger.Formatter
	originalHooks := logger.ReplaceHooks(make(log.LevelHooks))

	t.Cleanup(func() {
		logger.SetLevel(originalLevel)
		logger.SetOutput(originalOutput)
		logger.SetFormatter(originalFormatter)
		logger.ReplaceHooks(originalHooks)
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

	log.Debug("hidden diagnostic")
	log.Info("visible lifecycle event")

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

	log.WithFields(log.Fields{
		"component": "crawler",
		"operation": "complete",
		"app":       "overridden-app",
		"level":     "overridden-level",
		"msg":       "overridden-message",
		"time":      "overridden-time",
	}).Info("Crawler run completed")

	var event map[string]any
	require.NoError(t, json.Unmarshal(console.Bytes(), &event))
	assert.Equal(t, "INFO", event["level"])
	assert.Equal(t, "Crawler run completed", event["msg"])
	assert.Equal(t, "oss-register", event["app"])
	assert.Equal(t, "crawler", event["component"])
	assert.Equal(t, "complete", event["operation"])

	timestamp, ok := event["time"].(string)
	require.True(t, ok)
	parsedTime, err := time.Parse(time.RFC3339Nano, timestamp)
	require.NoError(t, err)
	assert.Equal(t, time.UTC, parsedTime.Location())
}

func TestConfigureLoggingSuppliesFallbackContext(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()

	var console bytes.Buffer
	closer, err := configureLogging(&console)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	log.Info("startup diagnostic")

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

	log.WithError(errors.New("connection refused")).Error("API request failed")

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

	log.Debug("configured diagnostic")

	assert.Contains(t, console.String(), "configured diagnostic")
}

func TestConfigureLoggingRejectsInvalidLevel(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()
	viper.Set("LOG_LEVEL", "verbose-ish")

	_, err := configureLogging(io.Discard)

	require.ErrorContains(t, err, "invalid LOG_LEVEL")
}

func TestConfigureLoggingRejectsTraceLevel(t *testing.T) {
	preserveGlobalLogger(t)
	viper.Reset()
	viper.Set("LOG_LEVEL", "trace")

	_, err := configureLogging(io.Discard)

	require.ErrorContains(t, err, "invalid LOG_LEVEL")
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

	log.Warn("degraded repository data")

	fileContents, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, console.String(), "degraded repository data")
	assert.Contains(t, string(fileContents), "degraded repository data")
}
