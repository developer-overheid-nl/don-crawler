package git

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/italia/publiccode-crawler/v4/common"
	"github.com/spf13/viper"
)

// CloneRepository clone the repository into DATADIR/repos/<hostname>/<vendor>/<repo>/gitClone.
func CloneRepository(hostname, name, gitURL, _ string) error {
	if name == "" {
		return errors.New("cannot save a file without name")
	}

	if gitURL == "" {
		return errors.New("cannot clone a repository without git URL")
	}

	vendor, repo := common.SplitFullName(name)
	path := filepath.Join(viper.GetString("DATADIR"), "repos", hostname, vendor, repo, "gitClone")
	authURL := withAuthToken(hostname, gitURL)

	// If folder already exists it will do a fetch instead of a clone.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		// Ensure remote URL has auth if needed.
		_, _ = exec.Command("git", "-C", path, "remote", "set-url", "origin", authURL).CombinedOutput()

		out, err := exec.Command("git", "-C", path, "fetch", "--all").CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot git pull the repository: %s: %w", out, err)
		}

		return nil
	}

	out, err := exec.Command("git", "clone", "--filter=blob:none", "--mirror", authURL, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot git clone the repository: %s: %w", out, err)
	}

	return err
}

func withAuthToken(hostname, gitURL string) string {
	u, err := url.Parse(gitURL)
	if err != nil {
		return gitURL
	}

	switch hostname {
	case "github.com":
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			u.User = url.UserPassword("x-access-token", token)
		}
	case "gitlab.com":
		if token := os.Getenv("GITLAB_TOKEN"); token != "" {
			u.User = url.UserPassword("oauth2", token)
		}
	default:
		// No-op for other hosts.
	}

	return u.String()
}
