package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostRepositoryIncludesForkFlag(t *testing.T) {
	var received repositoryRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/repositories", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"id":     "repo-1",
			"isFork": true,
		}))
	}))
	defer server.Close()

	client := APIClient{
		baseURL:         server.URL,
		retryableClient: server.Client(),
	}

	isFork := true
	created, err := client.PostRepository(
		"https://github.com/example/fork.git",
		nil,
		nil,
		nil,
		&isFork,
		"https://example.org/orgs/test",
		time.Time{},
		time.Time{},
		time.Time{},
	)
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.NotNil(t, received.IsFork)
	assert.True(t, *received.IsFork)
}
