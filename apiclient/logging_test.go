package apiclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/developer-overheid-nl/don-crawler/internal/loggingtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureAPIClientLogs(t *testing.T, level string) *loggingtest.Recorder {
	t.Helper()

	return loggingtest.Capture(t, level)
}

func clearKeycloakEnvironment(t *testing.T) {
	t.Helper()

	t.Setenv("KEYCLOAK_BASE_URL", "")
	t.Setenv("KEYCLOAK_REALM", "")
	t.Setenv("AUTH_CLIENT_ID", "")
	t.Setenv("AUTH_CLIENT_SECRET", "")
}

func TestNewClientLogsMissingOptionalAuthenticationAtDebug(t *testing.T) {
	recorder := captureAPIClientLogs(t, "debug")
	clearKeycloakEnvironment(t)

	NewClient()

	events := recorder.Events(t)
	require.Len(t, events, 1)
	assert.Equal(t, "DEBUG", events[0]["level"])
	assert.Contains(t, events[0]["msg"], "authentication is not configured")
}

func TestNewRetryableClientDisablesDefaultLogger(t *testing.T) {
	client := newRetryableClient()

	assert.Nil(t, client.Logger)
}

func TestPostRepositoryDoesNotLogPayloadAtDebug(t *testing.T) {
	recorder := captureAPIClientLogs(t, "debug")
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

	for _, event := range recorder.Events(t) {
		assert.NotContains(t, event["msg"], "payload=")
	}
}

func TestPostRepositoryDoesNotLogPayloadAsStructuredField(t *testing.T) {
	recorder := captureAPIClientLogs(t, "debug")
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

	for _, event := range recorder.Events(t) {
		assert.NotContains(t, event, "payload")
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
