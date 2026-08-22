package crawler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/developer-overheid-nl/don-crawler/internal/loggingtest"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureCrawlerLogs(t *testing.T) *loggingtest.Recorder {
	t.Helper()

	return loggingtest.Capture(t, "debug")
}

func TestEnsurePubliccodeFileLogsExpectedAbsenceOnceAtDebug(t *testing.T) {
	recorder := captureCrawlerLogs(t)
	repository := common.Repository{Name: "owner/repository"}

	new(Crawler).ensurePubliccodeFile(context.Background(), &repository)

	events := recorder.Events(t)
	require.Len(t, events, 1)
	assert.Equal(t, "DEBUG", events[0]["level"])
	assert.Contains(t, events[0]["msg"], "publiccode.yml not found")
}

func TestEnsurePubliccodeFileLogsNotFoundResponseOnceAtDebug(t *testing.T) {
	recorder := captureCrawlerLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	repository := common.Repository{Name: "owner/repository", FileRawURL: server.URL}

	new(Crawler).ensurePubliccodeFile(context.Background(), &repository)

	events := recorder.Events(t)
	require.Len(t, events, 1)
	assert.Equal(t, "DEBUG", events[0]["level"])
	assert.Contains(t, events[0]["msg"], "status: 404")
	assert.Empty(t, repository.FileRawURL)
}

func TestEnsurePubliccodeFileLogsServerFailureOnceAtWarning(t *testing.T) {
	recorder := captureCrawlerLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	repository := common.Repository{Name: "owner/repository", FileRawURL: server.URL}

	new(Crawler).ensurePubliccodeFile(context.Background(), &repository)

	events := recorder.Events(t)
	require.Len(t, events, 1)
	assert.Equal(t, "WARN", events[0]["level"])
	assert.Contains(t, events[0]["msg"], "status: 502")
	assert.Empty(t, repository.FileRawURL)
}

func TestEnsurePubliccodeFileLogsRequestFailureOnceAtWarning(t *testing.T) {
	recorder := captureCrawlerLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repository := common.Repository{
		Name:       "owner/repository",
		FileRawURL: "https://example.invalid/publiccode.yml",
	}

	new(Crawler).ensurePubliccodeFile(ctx, &repository)

	events := recorder.Events(t)
	require.Len(t, events, 1)
	assert.Equal(t, "WARN", events[0]["level"])
	assert.Contains(t, events[0]["msg"], "request failed")
	assert.Empty(t, repository.FileRawURL)
}

func TestEnsurePubliccodeFileLogsFoundFileAtDebug(t *testing.T) {
	recorder := captureCrawlerLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	repository := common.Repository{Name: "owner/repository", FileRawURL: server.URL}

	new(Crawler).ensurePubliccodeFile(context.Background(), &repository)

	events := recorder.Events(t)
	require.Len(t, events, 1)
	assert.Equal(t, "DEBUG", events[0]["level"])
	assert.Contains(t, events[0]["msg"], "publiccode.yml found")
	assert.Equal(t, server.URL, repository.FileRawURL)
}

func TestCloneAndLogActivityLogsFailureOnceAtWarning(t *testing.T) {
	recorder := captureCrawlerLogs(t)
	repository := common.Repository{Name: "owner/repository"}

	err := new(Crawler).cloneAndLogActivity(repository, "")

	require.Error(t, err)
	events := recorder.Events(t)
	require.Len(t, events, 1)
	assert.Equal(t, "WARN", events[0]["level"])
	assert.Contains(t, events[0]["msg"], "unable to determine clone URL")
}

func TestLastActivityFromGitLogsFallbackOnceAtWarning(t *testing.T) {
	recorder := captureCrawlerLogs(t)
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
	events := recorder.Events(t)
	require.Len(t, events, 2)
	assert.Equal(t, "DEBUG", events[0]["level"])
	assert.Equal(t, "WARN", events[1]["level"])
	assert.Contains(t, events[1]["msg"], "falling back to repository updated timestamp")
}

func TestLogPostRepositoryErrorEmitsSingleError(t *testing.T) {
	recorder := captureCrawlerLogs(t)

	logPostRepositoryError("owner/repository", errors.New("API rejected repository"))

	events := recorder.Events(t)
	require.Len(t, events, 1)
	assert.Equal(t, "ERROR", events[0]["level"])
	assert.Contains(t, events[0]["msg"], "[owner/repository]")
	assert.Contains(t, events[0]["msg"], "API rejected repository")
}
