package git

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/developer-overheid-nl/don-crawler/internal/loggingtest"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestCalculateRepoActivityReturnsSnapshotErrorWithoutContextlessLog(t *testing.T) {
	recorder := captureRepoActivityLogs(t)

	dataDir := t.TempDir()
	viper.Set("DATADIR", dataDir)
	repositoryPath := filepath.Join(dataDir, "repos", "github.com", "owner", "repository", "gitClone")
	require.NoError(t, os.MkdirAll(repositoryPath, 0o755))
	repository := common.Repository{
		Name: "owner/repository",
		URL:  url.URL{Host: "github.com"},
	}

	_, _, err := CalculateRepoActivity(repository, 60)

	require.Error(t, err)
	require.Empty(t, recorder.Events(t))
}

func TestCalculateRepoActivityLogsPartialCalculationWarningWithRepositoryContext(t *testing.T) {
	recorder := captureRepoActivityLogs(t)
	dataDir := t.TempDir()
	viper.Set("DATADIR", dataDir)
	repositoryPath := filepath.Join(dataDir, "repos", "github.com", "owner", "repository", "gitClone")
	require.NoError(t, os.MkdirAll(repositoryPath, 0o755))
	runGit(t, repositoryPath, "init", "--initial-branch=main")
	runGitWithCommitDate(t, repositoryPath, "2000-01-01T00:00:00Z", "commit", "--allow-empty", "-m", "old commit")

	originalWorkingDirectory, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(".."))
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalWorkingDirectory)) })
	repository := common.Repository{
		Name: "owner/repository",
		URL:  url.URL{Host: "github.com"},
	}

	_, _, err = CalculateRepoActivity(repository, 60)

	require.NoError(t, err)
	events := recorder.Events(t)
	require.Len(t, events, 1)
	require.Equal(t, "WARN", events[0]["level"])
	require.Contains(t, events[0]["msg"], "[owner/repository]")
	require.Contains(t, events[0]["msg"], "longevity unavailable")
}

func TestCalculateRepoActivityReturnsRangesErrorWithoutContextlessLog(t *testing.T) {
	recorder := captureRepoActivityLogs(t)
	dataDir := t.TempDir()
	viper.Set("DATADIR", dataDir)
	repositoryPath := filepath.Join(dataDir, "repos", "github.com", "owner", "repository", "gitClone")
	require.NoError(t, os.MkdirAll(repositoryPath, 0o755))
	runGit(t, repositoryPath, "init", "--initial-branch=main")
	runGitWithCommitDate(t, repositoryPath, "2026-08-01T00:00:00Z", "commit", "--allow-empty", "-m", "recent commit")

	originalWorkingDirectory, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalWorkingDirectory)) })
	repository := common.Repository{
		Name: "owner/repository",
		URL:  url.URL{Host: "github.com"},
	}

	_, _, err = CalculateRepoActivity(repository, 60)

	require.ErrorContains(t, err, "load vitality ranges")
	require.Empty(t, recorder.Events(t))
}

func TestLogPartialActivityErrorEmitsContextualWarning(t *testing.T) {
	recorder := captureRepoActivityLogs(t)

	logPartialActivityError("owner/repository", "tag activity", errors.New("invalid tag reference"))

	events := recorder.Events(t)
	require.Len(t, events, 1)
	require.Equal(t, "WARN", events[0]["level"])
	require.Contains(t, events[0]["msg"], "[owner/repository]")
	require.Contains(t, events[0]["msg"], "tag activity unavailable")
	require.Contains(t, events[0]["msg"], "invalid tag reference")
}

func captureRepoActivityLogs(t *testing.T) *loggingtest.Recorder {
	t.Helper()

	recorder := loggingtest.Capture(t, "debug")
	t.Cleanup(func() {
		viper.Reset()
	})

	return recorder
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git %v failed: %s", args, output)
}

func runGitWithCommitDate(t *testing.T, directory, date string, args ...string) {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=Logging Audit",
		"GIT_AUTHOR_EMAIL=logging-audit@example.invalid",
		"GIT_COMMITTER_NAME=Logging Audit",
		"GIT_COMMITTER_EMAIL=logging-audit@example.invalid",
		"GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_DATE="+date,
	)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "git %v failed: %s", args, output)
}
