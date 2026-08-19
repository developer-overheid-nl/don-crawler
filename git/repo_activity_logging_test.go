package git

import (
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/developer-overheid-nl/don-crawler/common"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestCalculateRepoActivityReturnsSnapshotErrorWithoutContextlessLog(t *testing.T) {
	hook := captureRepoActivityLogs(t)

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
	require.Empty(t, hook.AllEntries())
}

func TestCalculateRepoActivityLogsPartialCalculationWarningWithRepositoryContext(t *testing.T) {
	hook := captureRepoActivityLogs(t)
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
	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	require.Equal(t, log.WarnLevel, entries[0].Level)
	require.Contains(t, entries[0].Message, "[owner/repository]")
	require.Contains(t, entries[0].Message, "longevity unavailable")
}

func TestCalculateRepoActivityReturnsRangesErrorWithoutContextlessLog(t *testing.T) {
	hook := captureRepoActivityLogs(t)
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
	require.Empty(t, hook.AllEntries())
}

func TestLogPartialActivityErrorEmitsContextualWarning(t *testing.T) {
	hook := captureRepoActivityLogs(t)

	logPartialActivityError("owner/repository", "tag activity", errors.New("invalid tag reference"))

	entries := hook.AllEntries()
	require.Len(t, entries, 1)
	require.Equal(t, log.WarnLevel, entries[0].Level)
	require.Contains(t, entries[0].Message, "[owner/repository]")
	require.Contains(t, entries[0].Message, "tag activity unavailable")
	require.Contains(t, entries[0].Message, "invalid tag reference")
}

func captureRepoActivityLogs(t *testing.T) *logtest.Hook {
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
		viper.Reset()
	})

	return hook
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
