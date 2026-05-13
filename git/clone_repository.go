package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/developer-overheid-nl/don-crawler/common"
	githubapp "github.com/developer-overheid-nl/don-crawler/internal/githubapp"
	git "github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
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
	depth := cloneDepth()
	refName, refSpecs := branchRef(branch)

	auth, err := withAuthToken(hostname, gitURL)
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
			RefSpecs:   refSpecs,
			Depth:      depth,
			Tags:       git.TagFollowing,
			Force:      true,
			Prune:      true,
		}
		if err := repo.Fetch(fetchOpts); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("cannot fetch the repository: %w", err)
		}

		return nil
	}

	_, err = git.PlainClone(path, true, &git.CloneOptions{
		URL:           gitURL,
		Auth:          auth,
		ReferenceName: refName,
		SingleBranch:  branch != "",
		Depth:         depth,
		Tags:          git.TagFollowing,
	})
	if err != nil {
		return fmt.Errorf("cannot git clone the repository: %w", err)
	}

	return err
}

func cloneDepth() int {
	days := viper.GetInt("ACTIVITY_DAYS")
	if days < 1 {
		return 1
	}

	return days
}

func branchRef(branch string) (plumbing.ReferenceName, []gitcfg.RefSpec) {
	if branch == "" {
		return "", nil
	}

	ref := plumbing.NewBranchReferenceName(branch)

	return ref, []gitcfg.RefSpec{
		gitcfg.RefSpec(fmt.Sprintf("+%s:%s", ref, ref)),
	}
}

func withAuthToken(hostname, _ string) (transport.AuthMethod, error) {
	switch hostname {
	case "github.com":
		provider, err := githubapp.DefaultProvider()
		if err != nil {
			return nil, fmt.Errorf("github app auth unavailable: %w", err)
		}

		if provider != nil {
			token, _, err := provider.Token(context.Background())
			if err != nil {
				return nil, fmt.Errorf("github app token fetch failed: %w", err)
			}

			return &githttp.BasicAuth{
				Username: "x-access-token",
				Password: token,
			}, nil
		}

		return nil, errors.New("github app auth not configured for github.com")
	case "gitlab.com":
		//nolint
		return nil, nil
	default:
		// No-op for other hosts.
	}

	return nil, fmt.Errorf("no auth method available for host %s", hostname)
}
