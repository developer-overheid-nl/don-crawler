package scanner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBitbucketArchivedFromPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    bool
	}{
		{
			name:    "archived",
			payload: map[string]any{"archived": true},
			want:    true,
		},
		{
			name:    "is_archived",
			payload: map[string]any{"is_archived": true},
			want:    true,
		},
		{
			name:    "isArchived",
			payload: map[string]any{"isArchived": true},
			want:    true,
		},
		{
			name:    "missing",
			payload: map[string]any{"name": "repo"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bitbucketArchivedFromPayload(tt.payload))
		})
	}
}

func TestBitbucketRepositoryArchiveStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/2.0/repositories/example/repo", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprint(w, `{"archived": true}`)
		require.NoError(t, err)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_API_BASE_URL", server.URL+"/2.0")

	archived, err := bitbucketRepositoryArchiveStatus(context.Background(), server.Client(), "example", "repo")
	require.NoError(t, err)
	assert.True(t, archived)
}
