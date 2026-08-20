package apiclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureAPIClientLogs(t *testing.T, level log.Level) *logtest.Hook {
	t.Helper()

	logger := log.StandardLogger()
	originalLevel := logger.GetLevel()
	originalOutput := logger.Out
	originalHooks := logger.ReplaceHooks(make(log.LevelHooks))
	logger.SetLevel(level)
	logger.SetOutput(io.Discard)
	hook := logtest.NewGlobal()

	t.Cleanup(func() {
		logger.SetLevel(originalLevel)
		logger.SetOutput(originalOutput)
		logger.ReplaceHooks(originalHooks)
	})

	return hook
}

func clearKeycloakEnvironment(t *testing.T) {
	t.Helper()

	t.Setenv("KEYCLOAK_BASE_URL", "")
	t.Setenv("KEYCLOAK_REALM", "")
	t.Setenv("AUTH_CLIENT_ID", "")
	t.Setenv("AUTH_CLIENT_SECRET", "")
}

func TestNewClientLogsMissingOptionalAuthenticationAtDebug(t *testing.T) {
	hook := captureAPIClientLogs(t, log.DebugLevel)
	clearKeycloakEnvironment(t)

	NewClient()

	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, log.DebugLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "authentication is not configured")
}

func TestNewRetryableClientDisablesDefaultLogger(t *testing.T) {
	client := newRetryableClient()

	assert.Nil(t, client.Logger)
}

func TestPostRepositoryDoesNotLogPayloadAtDebug(t *testing.T) {
	hook := captureAPIClientLogs(t, log.DebugLevel)
	client, closeServer := successfulRepositoryAPIClient(t)
	t.Cleanup(closeServer)

	_, err := client.PostRepository(
		"https://github.com/example/repository.git",
		nil,
		nil,
		nil,
		nil,
		nil,
		"https://example.invalid/organisation",
		time.Time{},
		time.Time{},
		time.Time{},
	)
	require.NoError(t, err)

	for _, entry := range hook.AllEntries() {
		assert.NotContains(t, entry.Message, "payload=")
	}
}

func TestPostRepositoryDoesNotLogPayloadAtTrace(t *testing.T) {
	hook := captureAPIClientLogs(t, log.TraceLevel)
	client, closeServer := successfulRepositoryAPIClient(t)
	t.Cleanup(closeServer)

	_, err := client.PostRepository(
		"https://github.com/example/repository.git",
		nil,
		nil,
		nil,
		nil,
		nil,
		"https://example.invalid/organisation",
		time.Time{},
		time.Time{},
		time.Time{},
	)
	require.NoError(t, err)

	for _, entry := range hook.AllEntries() {
		assert.NotContains(t, entry.Data, "payload")
	}
}

func TestJoinPathReturnsInvalidBaseURLToCaller(t *testing.T) {
	_, err := joinPath("https://example.invalid/%zz", "/repositories")

	require.Error(t, err)
}

func successfulRepositoryAPIClient(t *testing.T) (APIClient, func()) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"repository-id"}`))
	}))

	return APIClient{
		baseURL:         server.URL,
		retryableClient: server.Client(),
	}, server.Close
}
