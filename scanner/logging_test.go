package scanner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/google/go-github/v43/github"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureScannerLogs(t *testing.T) *logtest.Hook {
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

func githubScannerForServer(t *testing.T, server *httptest.Server) GitHubScanner {
	t.Helper()

	client := github.NewClient(server.Client())
	baseURL, err := url.Parse(server.URL + "/")
	require.NoError(t, err)
	client.BaseURL = baseURL

	return GitHubScanner{client: client, ctx: context.Background()}
}

func TestGitHubUserFallbackLogsOnlyDeprecationAtWarning(t *testing.T) {
	hook := captureScannerLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/octocat/repos":
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
		case "/users/octocat/repos":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	scanner := githubScannerForServer(t, server)
	groupURL, err := url.Parse("https://github.com/octocat")
	require.NoError(t, err)

	err = scanner.ScanGroupOfRepos(*groupURL, common.Publisher{}, make(chan common.Repository, 1))

	require.NoError(t, err)
	var warningEntries int
	for _, entry := range hook.AllEntries() {
		if entry.Level == log.WarnLevel {
			warningEntries++
			assert.Contains(t, entry.Message, "listing repos as GitHub user")
		}
	}
	assert.Equal(t, 1, warningEntries)
}

func TestGitHubPrivateRepositoryReturnsExpectedSkip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/private" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"private":true,"full_name":"example/private"}`)
	}))
	t.Cleanup(server.Close)
	scanner := githubScannerForServer(t, server)
	repositoryURL, err := url.Parse("https://github.com/example/private")
	require.NoError(t, err)

	err = scanner.ScanRepo(*repositoryURL, common.Publisher{}, make(chan common.Repository, 1))

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRepositorySkipped))
}

func TestGitHubMissingPubliccodeDoesNotLogWarning(t *testing.T) {
	hook := captureScannerLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example/public":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{
				"private":false,
				"full_name":"example/public",
				"name":"public",
				"clone_url":"https://github.com/example/public.git",
				"default_branch":"main"
			}`)
		case "/repos/example/public/contents/publiccode.yml":
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	scanner := githubScannerForServer(t, server)
	repositoryURL, err := url.Parse("https://github.com/example/public")
	require.NoError(t, err)
	repositories := make(chan common.Repository, 1)

	err = scanner.ScanRepo(*repositoryURL, common.Publisher{}, repositories)

	require.NoError(t, err)
	require.Len(t, repositories, 1)
	for _, entry := range hook.AllEntries() {
		assert.NotEqual(t, log.WarnLevel, entry.Level)
	}
}
