package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/developer-overheid-nl/don-crawler/common"
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

const defaultBitbucketAPIBaseURL = "https://api.bitbucket.org/2.0"

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
				IsFork:       bitbucketRepositoryIsFork(&r),
				IsArchived:   scanner.bitbucketRepositoryIsArchived(owner, r.Slug),
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
			IsFork:       bitbucketRepositoryIsFork(repo),
			IsArchived:   scanner.bitbucketRepositoryIsArchived(owner, repo.Slug),
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
	// implement Bitbucket last commit lookup when we have Bitbucket repos.
	return time.Time{}, errors.New("bitbucket last commit lookup not implemented")
}

func bitbucketRepositoryIsFork(repo *bitbucket.Repository) bool {
	return repo != nil && repo.Parent != nil
}

func (scanner BitBucketScanner) bitbucketRepositoryIsArchived(owner, slug string) bool {
	archived, err := bitbucketRepositoryArchiveStatus(context.Background(), scanner.client.HttpClient, owner, slug)
	if err != nil {
		log.Warnf("failed to get Bitbucket archived status for %s/%s: %v", owner, slug, err)

		return false
	}

	return archived
}

func bitbucketRepositoryArchiveStatus(ctx context.Context, client *http.Client, owner, slug string) (bool, error) {
	endpoint, err := bitbucketRepositoryAPIURL(owner, slug)
	if err != nil {
		return false, err
	}

	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}

	res, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))

		return false, fmt.Errorf("bitbucket API replied with HTTP %s: %s", res.Status, strings.TrimSpace(string(body)))
	}

	var payload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return false, err
	}

	return bitbucketArchivedFromPayload(payload), nil
}

func bitbucketRepositoryAPIURL(owner, slug string) (string, error) {
	baseRaw := strings.TrimSpace(os.Getenv("BITBUCKET_API_BASE_URL"))
	if baseRaw == "" {
		baseRaw = defaultBitbucketAPIBaseURL
	}

	baseURL, err := url.Parse(baseRaw)
	if err != nil {
		return "", err
	}

	baseURL.Path = path.Join(baseURL.Path, "repositories", owner, slug)

	return baseURL.String(), nil
}

func bitbucketArchivedFromPayload(payload map[string]any) bool {
	for _, key := range []string{"archived", "is_archived", "isArchived"} {
		if archived, ok := payload[key].(bool); ok {
			return archived
		}
	}

	return false
}
