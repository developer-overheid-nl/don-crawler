package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/spf13/viper"
)

// CloneRepository clones the repository into DATADIR/repos/<hostname>/<vendor>/<repo>/gitClone.
func CloneRepository(hostname, name, gitURL, branch string) error {
	if name == "" {
		return errors.New("cannot save a file without name")
	}

	if gitURL == "" {
		return errors.New("cannot clone a repository without git URL")
	}

	vendor, repo := common.SplitFullName(name)

	path := filepath.Join(viper.GetString("DATADIR"), "repos", hostname, vendor, repo, "gitClone")
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("cannot remove existing git repository: %w", err)
	}

	args := []string{
		"clone",
		"--bare",
		"--depth", fmt.Sprint(cloneDepth()),
		"--no-tags",
	}

	if branch != "" {
		args = append(args, "--single-branch", "--branch", branch)
	}

	args = append(args, gitURL, path)

	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot git clone the repository: %s: %w", out, err)
	}

	return nil
}

// RemoveRepository removes the local clone for a repository.
func RemoveRepository(hostname, name string) error {
	if name == "" {
		return errors.New("cannot remove a repository without name")
	}

	vendor, repo := common.SplitFullName(name)
	path := filepath.Join(viper.GetString("DATADIR"), "repos", hostname, vendor, repo, "gitClone")

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("cannot remove git repository: %w", err)
	}

	return nil
}

func cloneDepth() int {
	days := viper.GetInt("ACTIVITY_DAYS")
	if days < 1 {
		return 1
	}

	return days
}
