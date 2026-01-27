package git

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/italia/publiccode-crawler/v4/common"
	githubapp "github.com/italia/publiccode-crawler/v4/internal/githubapp"
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
	authURL, err := withAuthToken(hostname, gitURL)
	if err != nil {
		return err
	}

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

func withAuthToken(hostname, gitURL string) (string, error) {
	u, err := url.Parse(gitURL)
	if err != nil {
		return gitURL, nil
	}

	switch hostname {
	case "github.com":
		provider, err := githubapp.DefaultProvider()
		if err != nil {
			return "", fmt.Errorf("github app auth unavailable: %w", err)
		}
		if provider != nil {
			token, _, err := provider.Token(context.Background())
			if err != nil {
				return "", fmt.Errorf("github app token fetch failed: %w", err)
			}
			u.User = url.UserPassword("x-access-token", token)
			return u.String(), nil
		}
		return "", errors.New("github app auth not configured for github.com")
	case "gitlab.com":
		if token := os.Getenv("GITLAB_TOKEN"); token != "" {
			u.User = url.UserPassword("oauth2", token)
		}
	default:
		// No-op for other hosts.
	}

	return u.String(), nil
}
