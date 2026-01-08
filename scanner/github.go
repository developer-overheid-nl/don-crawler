package scanner

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v43/github"
	"github.com/italia/publiccode-crawler/v4/common"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

type GitHubScanner struct {
	client *github.Client
	ctx    context.Context
}

var githubCommitRateLimit = struct {
	mu    sync.Mutex
	reset time.Time
}{}

// NewGitHubScanner returns a new GitHubScanner using the
// authentication token from the GITHUB_TOKEN environment variable or,
// if not set, the tokens in domains.yml.
func NewGitHubScanner() Scanner {
	ctx := context.Background()
	token := os.Getenv("GITHUB_TOKEN")

	var httpClient *http.Client
	if token == "" {
		log.Infof("GitHub API auth: GITHUB_TOKEN not set; using unauthenticated client")
		httpClient = http.DefaultClient
	} else {
		log.Infof("GitHub API auth: GITHUB_TOKEN set; using authenticated client")
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		httpClient = oauth2.NewClient(ctx, ts)
	}

	client := github.NewClient(httpClient)

	return GitHubScanner{client: client, ctx: ctx}
}

// ScanGroupOfRepos scans a GitHub organization represented by url, associated to
// publisher and sends any repository containing a publiccode.yml to the repositories
// channel as a [common.Repository].
// It returns any error encountered if any, otherwise nil.
func (scanner GitHubScanner) ScanGroupOfRepos(
	url url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	log.Debugf("GitHubScanner.ScanGroupOfRepos(%s)", url.String())

	opt := &github.RepositoryListByOrgOptions{}

	splitted := strings.Split(strings.Trim(url.Path, "/"), "/")
	if len(splitted) != 1 {
		return fmt.Errorf("doesn't look like a GitHub org %s", url.String())
	}

	orgName := splitted[0]

	for {
	Retry:
		repos, resp, err := scanner.client.Repositories.ListByOrg(scanner.ctx, orgName, opt)

		var rateLimitError *github.RateLimitError
		if errors.As(err, &rateLimitError) {
			log.Infof("GitHub rate limit hit, sleeping until %s", resp.Rate.Reset.Time.String())
			time.Sleep(time.Until(resp.Rate.Reset.Time))

			goto Retry
		}

		var abuseRateLimitError *github.AbuseRateLimitError
		if errors.As(err, &abuseRateLimitError) {
			secondaryRateLimit(abuseRateLimitError)

			goto Retry
		}

		if err != nil {
			// Try to list repos by user, for backwards compatibility.
			log.Warnf(
				"can't list repositories in %s (not an GitHub organization?): %s",
				url.String(), err.Error(),
			)

			repos, resp, err = scanner.client.Repositories.List(scanner.ctx, orgName, nil)
			if err != nil {
				return fmt.Errorf("can't list repositories in %s (not an GitHub organization?): %w", url.String(), err)
			}

			log.Warnf(
				"%s is not a GitHub organization, listing repos as GitHub user. This will be removed in the future",
				url.String(),
			)
		}

		// Add repositories to the channel that will perform the check on everyone.
		for _, r := range repos {
			if isDotGitHubRepoName(r.GetName()) {
				repoRef := r.GetHTMLURL()
				if repoRef == "" {
					repoRef = r.GetFullName()
				}

				if repoRef == "" {
					repoRef = ".github"
				}
				log.Debugf("Skipping GitHub .github repository: %s", repoRef)

				continue
			}

			repoURL, err := url.Parse(*r.HTMLURL)
			if err != nil {
				log.Errorf("can't parse URL %s: %s", *r.URL, err.Error())

				continue
			}

			if err = scanner.ScanRepo(*repoURL, publisher, repositories); err != nil {
				if errors.Is(err, ErrPubliccodeNotFound) {
					log.Warnf("can't scan repository %s: %s", repoURL.String(), err.Error())
				} else {
					log.Errorf("can't scan repository %s: %s", repoURL.String(), err.Error())
				}

				continue
			}
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}

	return nil
}

// ScanRepo scans a GitHub repository represented by url, associated to
// publisher and, if it contains a publiccode.yml, sends it as a [common.Repository]
// repositories channel.
// It returns any error encountered if any, otherwise nil.
func (scanner GitHubScanner) ScanRepo(
	url url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	log.Debugf("GitHubScanner.ScanRepo(%s)", url.String())

	splitted := strings.Split(strings.Trim(url.Path, "/"), "/")
	if len(splitted) != 2 {
		return fmt.Errorf("doesn't look like a GitHub repo %s", url.String())
	}

	orgName, repoName := splitted[0], splitted[1]
	if isDotGitHubRepoName(repoName) {
		log.Debugf("Skipping GitHub .github repository: %s", url.String())

		return nil
	}

Retry:
	repo, resp, err := scanner.client.Repositories.Get(scanner.ctx, orgName, repoName)

	var rateLimitError *github.RateLimitError
	if errors.As(err, &rateLimitError) {
		log.Infof("GitHub rate limit hit, sleeping until %s", resp.Rate.Reset.Time.String())
		time.Sleep(time.Until(resp.Rate.Reset.Time))

		goto Retry
	}

	var abuseRateLimitError *github.AbuseRateLimitError
	if errors.As(err, &abuseRateLimitError) {
		secondaryRateLimit(abuseRateLimitError)

		goto Retry
	}

	if err != nil {
		return fmt.Errorf("can't get repo %s: %w", url.String(), err)
	}

	if *repo.Private || *repo.Archived {
		return fmt.Errorf("skipping private or archived repo %s", *repo.FullName)
	}

	file, _, resp, err := scanner.client.Repositories.GetContents(scanner.ctx, orgName, repoName, "publiccode.yml", nil)
	if errors.As(err, &rateLimitError) {
		log.Infof("GitHub rate limit hit, sleeping until %s", resp.Rate.Reset.Time.String())
		time.Sleep(time.Until(resp.Rate.Reset.Time))

		goto Retry
	}

	if errors.As(err, &abuseRateLimitError) {
		secondaryRateLimit(abuseRateLimitError)

		goto Retry
	}

	var fileRawURL string

	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			log.Warnf(
				"[%s]: publiccode.yml not found on branch %s",
				*repo.FullName,
				repo.GetDefaultBranch(),
			)
		} else {
			return fmt.Errorf("[%s]: failed to get publiccode.yml: %w", *repo.FullName, err)
		}
	} else {
		if file.DownloadURL == nil {
			log.Warnf("[%s]: failed to get publiccode.yml: not a regular file?", *repo.FullName)
		} else {
			fileRawURL = *file.DownloadURL
		}
	}

	canonicalURL, err := url.Parse(*repo.CloneURL)
	if err != nil {
		return fmt.Errorf("failed to get canonical repo URL for %s: %w", url.String(), err)
	}

	repositories <- common.Repository{
		Name:         *repo.FullName,
		Title:        repo.GetName(),
		Description:  repo.GetDescription(),
		FileRawURL:   fileRawURL,
		URL:          url,
		CanonicalURL: *canonicalURL,
		GitBranch:    *repo.DefaultBranch,
		CreatedAt:    repo.GetCreatedAt().Time,
		UpdatedAt:    repo.GetUpdatedAt().Time,
		Publisher:    publisher,
		Headers:      make(map[string]string),
	}

	return nil
}

// LastCommitTimeFromAPI returns the last commit time for a GitHub repository.
func (scanner GitHubScanner) LastCommitTimeFromAPI(repoURL url.URL) (time.Time, error) {
	return lastCommitTimeWithRetry("github", func() (time.Time, error) {
		return scanner.lastCommitTimeFromAPI(repoURL)
	})
}

func (scanner GitHubScanner) lastCommitTimeFromAPI(repoURL url.URL) (time.Time, error) {
	owner, repo, err := splitRepoOwnerAndName(repoURL)
	if err != nil {
		return time.Time{}, err
	}

	if reset := githubCommitRateLimitReset(); !reset.IsZero() {
		return time.Time{}, RateLimitError{Provider: "github", Reset: reset}
	}

	opts := &github.CommitsListOptions{
		ListOptions: github.ListOptions{PerPage: 1},
	}

	commits, _, err := scanner.client.Repositories.ListCommits(scanner.ctx, owner, repo, opts)
	if err != nil {
		var rateLimitError *github.RateLimitError
		if errors.As(err, &rateLimitError) {
			reset := rateLimitError.Rate.Reset.Time
			setGitHubCommitRateLimit(reset)

			return time.Time{}, RateLimitError{Provider: "github", Reset: reset}
		}

		var abuseRateLimitError *github.AbuseRateLimitError
		if errors.As(err, &abuseRateLimitError) {
			reset := time.Now().Add(githubAbuseRetryAfter(abuseRateLimitError))
			setGitHubCommitRateLimit(reset)

			return time.Time{}, RateLimitError{Provider: "github", Reset: reset}
		}

		return time.Time{}, err
	}

	if len(commits) == 0 || commits[0].Commit == nil {
		return time.Time{}, errors.New("no commits found")
	}

	commit := commits[0].Commit
	if commit.Committer != nil && commit.Committer.Date != nil {
		return *commit.Committer.Date, nil
	}

	if commit.Author != nil && commit.Author.Date != nil {
		return *commit.Author.Date, nil
	}

	return time.Time{}, errors.New("commit date missing")
}

func secondaryRateLimit(err *github.AbuseRateLimitError) {
	var duration time.Duration
	if err.RetryAfter != nil {
		duration = *err.RetryAfter
	} else {
		duration = time.Duration(rand.Intn(100)) * time.Second //nolint:gosec
	}

	log.Infof("GitHub secondary rate limit hit, for %s", duration)
	time.Sleep(duration)
}

func githubCommitRateLimitReset() time.Time {
	githubCommitRateLimit.mu.Lock()
	defer githubCommitRateLimit.mu.Unlock()

	if githubCommitRateLimit.reset.IsZero() {
		return time.Time{}
	}

	if !time.Now().Before(githubCommitRateLimit.reset) {
		githubCommitRateLimit.reset = time.Time{}

		return time.Time{}
	}

	return githubCommitRateLimit.reset
}

func setGitHubCommitRateLimit(reset time.Time) {
	if reset.IsZero() {
		return
	}

	githubCommitRateLimit.mu.Lock()
	defer githubCommitRateLimit.mu.Unlock()

	if reset.After(githubCommitRateLimit.reset) {
		githubCommitRateLimit.reset = reset
	}
}

func githubAbuseRetryAfter(err *github.AbuseRateLimitError) time.Duration {
	if err == nil || err.RetryAfter == nil {
		return 30 * time.Second
	}

	return *err.RetryAfter
}

func isDotGitHubRepoName(repoName string) bool {
	repoNameNormalized := strings.TrimSuffix(repoName, ".git")

	return strings.EqualFold(repoNameNormalized, ".github")
}
