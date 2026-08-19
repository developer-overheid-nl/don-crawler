package crawler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/developer-overheid-nl/don-crawler/common"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureCrawlerLogs(t *testing.T) *logtest.Hook {
	t.Helper()

	logger := log.StandardLogger()
	originalLevel := logger.GetLevel()
	originalOutput := logger.Out
	originalHooks := logger.ReplaceHooks(make(log.LevelHooks))
	logger.SetLevel(log.DebugLevel)
	logger.SetOutput(io.Discard)
	hook := logtest.NewGlobal()

	t.Cleanup(func() {
		logger.SetLevel(originalLevel)
		logger.SetOutput(originalOutput)
		logger.ReplaceHooks(originalHooks)
	})

	return hook
}

func TestEnsurePubliccodeFileLogsExpectedAbsenceOnceAtDebug(t *testing.T) {
	hook := captureCrawlerLogs(t)
	repository := common.Repository{Name: "owner/repository"}

	new(Crawler).ensurePubliccodeFile(context.Background(), &repository)

	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, log.DebugLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "publiccode.yml not found")
}

func TestEnsurePubliccodeFileLogsNotFoundResponseOnceAtDebug(t *testing.T) {
	hook := captureCrawlerLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	repository := common.Repository{Name: "owner/repository", FileRawURL: server.URL}

	new(Crawler).ensurePubliccodeFile(context.Background(), &repository)

	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, log.DebugLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "status: 404")
	assert.Empty(t, repository.FileRawURL)
}

func TestEnsurePubliccodeFileLogsServerFailureOnceAtWarning(t *testing.T) {
	hook := captureCrawlerLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	repository := common.Repository{Name: "owner/repository", FileRawURL: server.URL}

	new(Crawler).ensurePubliccodeFile(context.Background(), &repository)

	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, log.WarnLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "status: 502")
	assert.Empty(t, repository.FileRawURL)
}

func TestEnsurePubliccodeFileLogsRequestFailureOnceAtWarning(t *testing.T) {
	hook := captureCrawlerLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repository := common.Repository{
		Name:       "owner/repository",
		FileRawURL: "https://example.invalid/publiccode.yml",
	}

	new(Crawler).ensurePubliccodeFile(ctx, &repository)

	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, log.WarnLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "request failed")
	assert.Empty(t, repository.FileRawURL)
}

func TestEnsurePubliccodeFileLogsFoundFileAtDebug(t *testing.T) {
	hook := captureCrawlerLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	repository := common.Repository{Name: "owner/repository", FileRawURL: server.URL}

	new(Crawler).ensurePubliccodeFile(context.Background(), &repository)

	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, log.DebugLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "publiccode.yml found")
	assert.Equal(t, server.URL, repository.FileRawURL)
}

func TestCloneAndLogActivityLogsFailureOnceAtWarning(t *testing.T) {
	hook := captureCrawlerLogs(t)
	repository := common.Repository{Name: "owner/repository"}

	err := new(Crawler).cloneAndLogActivity(repository, "")

	require.Error(t, err)
	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, log.WarnLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "unable to determine clone URL")
}

func TestLastActivityFromGitLogsFallbackOnceAtWarning(t *testing.T) {
	hook := captureCrawlerLogs(t)
	viper.Set("DATADIR", t.TempDir())
	t.Cleanup(viper.Reset)
	updatedAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	repository := common.Repository{
		Name:      "owner/repository",
		URL:       url.URL{Host: "github.com"},
		UpdatedAt: updatedAt,
	}

	lastActivity := new(Crawler).lastActivityFromGit(repository, errors.New("clone failed"))

	assert.Equal(t, updatedAt, lastActivity)
	entries := hook.AllEntries()
	require.Len(t, entries, 2)
	assert.Equal(t, log.DebugLevel, entries[0].Level)
	assert.Equal(t, log.WarnLevel, entries[1].Level)
	assert.Contains(t, entries[1].Message, "falling back to repository updated timestamp")
}

func TestLogPostRepositoryErrorEmitsSingleError(t *testing.T) {
	hook := captureCrawlerLogs(t)

	logPostRepositoryError("owner/repository", errors.New("API rejected repository"))

	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, log.ErrorLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "[owner/repository]")
	assert.Contains(t, entries[0].Message, "API rejected repository")
}
