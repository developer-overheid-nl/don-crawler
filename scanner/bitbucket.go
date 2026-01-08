package scanner

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/italia/publiccode-crawler/v4/common"
	"github.com/ktrysmt/go-bitbucket"
	log "github.com/sirupsen/logrus"
)

type BitBucketScanner struct {
	client *bitbucket.Client
}

func NewBitBucketScanner() Scanner {
	client, _ := bitbucket.NewBasicAuth("", "")

	return BitBucketScanner{client: client}
}

func bitbucketTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}

	return *t
}

// RegisterBitbucketAPI register the crawler function for Bitbucket API.
func (scanner BitBucketScanner) ScanGroupOfRepos(
	url url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	log.Debugf("BitBucketScanner.ScanGroupOfRepos(%s)", url.String())

	splitted := strings.Split(strings.Trim(url.Path, "/"), "/")

	if len(splitted) != 1 {
		return fmt.Errorf("bitbucket URL %s doesn't look like a group of repos", url.String())
	}

	owner := splitted[0]

	opt := &bitbucket.RepositoriesOptions{
		Owner: owner,
	}

	res, err := scanner.client.Repositories.ListForAccount(opt)
	if err != nil {
		return fmt.Errorf("can't list repositories in %s: %w", url.String(), err)
	}

	for _, r := range res.Items {
		if r.Is_private {
			log.Warnf("Skipping %s: repo is private", r.Full_name)

			continue
		}

		opt := &bitbucket.RepositoryFilesOptions{
			Owner:    owner,
			RepoSlug: r.Slug,
			Ref:      r.Mainbranch.Name,
			Path:     "publiccode.yml",
		}

		res, err := scanner.client.Repositories.Repository.GetFileContent(opt)
		if err != nil {
			log.Infof("[%s]: no publiccode.yml: %s", r.Full_name, err.Error())

			continue
		}

		if res != nil {
			u, err := url.Parse(fmt.Sprintf("https://bitbucket.org/%s/%s.git", owner, r.Slug))
			if err != nil {
				return fmt.Errorf("failed to get canonical repo URL for %s: %w", url.String(), err)
			}

			repositories <- common.Repository{
				Name:         r.Full_name,
				Title:        r.Name,
				Description:  r.Description,
				FileRawURL:   fmt.Sprintf("https://bitbucket.org/%s/%s/raw/%s/publiccode.yml", owner, r.Slug, r.Mainbranch.Name),
				URL:          *u,
				CanonicalURL: *u,
				GitBranch:    r.Mainbranch.Name,
				CreatedAt:    bitbucketTime(r.CreatedOnTime),
				UpdatedAt:    bitbucketTime(r.UpdatedOnTime),
				Publisher:    publisher,
			}
		}
	}

	return nil
}

// RegisterSingleBitbucketAPI register the crawler function for single Bitbucket repository.
func (scanner BitBucketScanner) ScanRepo(
	url url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	log.Debugf("BitBucketScanner.ScanRepo(%s)", url.String())

	splitted := strings.Split(strings.Trim(url.Path, "/"), "/")
	if len(splitted) != 2 {
		return fmt.Errorf("bitbucket URL %s doesn't look like a repo", url.String())
	}

	owner := splitted[0]
	slug := splitted[1]

	opt := &bitbucket.RepositoryOptions{
		Owner:    owner,
		RepoSlug: slug,
	}

	repo, err := scanner.client.Repositories.Repository.Get(opt)
	if err != nil {
		return err
	}

	filesOpt := &bitbucket.RepositoryFilesOptions{
		Owner:    owner,
		RepoSlug: slug,
		Ref:      "HEAD",
		Path:     "publiccode.yml",
	}

	res, err := scanner.client.Repositories.Repository.GetFileContent(filesOpt)
	if err != nil {
		return fmt.Errorf("[%s]: no publiccode.yml: %w", url.String(), err)
	}

	if res != nil {
		canonicalURL, err := url.Parse(fmt.Sprintf("https://bitbucket.org/%s/%s.git", owner, repo.Slug))
		if err != nil {
			return fmt.Errorf("failed to get canonical repo URL for %s: %w", url.String(), err)
		}

		repositories <- common.Repository{
			Name:         repo.Full_name,
			Title:        repo.Name,
			Description:  repo.Description,
			FileRawURL:   fmt.Sprintf("https://bitbucket.org/%s/%s/raw/%s/publiccode.yml", owner, slug, repo.Mainbranch.Name),
			URL:          url,
			CanonicalURL: *canonicalURL,
			GitBranch:    repo.Mainbranch.Name,
			CreatedAt:    bitbucketTime(repo.CreatedOnTime),
			UpdatedAt:    bitbucketTime(repo.UpdatedOnTime),
			Publisher:    publisher,
		}
	}

	return nil
}

// LastCommitTimeFromAPI returns the last commit time for a Bitbucket repository.
func (scanner BitBucketScanner) LastCommitTimeFromAPI(_ url.URL) (time.Time, error) {
	// TODO: implement Bitbucket last commit lookup when we have Bitbucket repos.
	return time.Time{}, errors.New("bitbucket last commit lookup not implemented")
}
