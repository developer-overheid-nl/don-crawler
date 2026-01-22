package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
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
	auth := gitAuth(hostname)

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

func gitAuth(hostname string) transport.AuthMethod {
	switch hostname {
	case "github.com":
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			return &githttp.BasicAuth{
				Username: "x-access-token",
				Password: token,
			}
		}
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

	return nil
}
