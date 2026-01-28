package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
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
		repo, err := git.PlainOpen(path)
		if err != nil {
			return fmt.Errorf("cannot open git repository: %w", err)
		}

		fetchOpts := &git.FetchOptions{
			RemoteName: git.DefaultRemoteName,
			RemoteURL:  gitURL,
			Auth:       auth,
			RefSpecs:   []gitcfg.RefSpec{"+refs/*:refs/*"},
			Tags:       git.AllTags,
			Force:      true,
			Prune:      true,
		}
		if err := repo.Fetch(fetchOpts); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("cannot fetch the repository: %w", err)
		}

		return nil
	}

	_, err := git.PlainClone(path, true, &git.CloneOptions{
		URL:    gitURL,
		Auth:   auth,
		Mirror: true,
		Tags:   git.AllTags,
	})
	if err != nil {
		return fmt.Errorf("cannot git clone the repository: %w", err)
	}

	return err
}

func withAuthToken(hostname, gitURL string) (string, error) {
	u, err := url.Parse(gitURL)
	if err != nil {
		return gitURL, fmt.Errorf("invalid git URL %q: %w", gitURL, err)
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
			return &githttp.BasicAuth{
				Username: "oauth2",
				Password: token,
			}
		}
	default:
		// No-op for other hosts.
	}

	return u.String(), nil
}
