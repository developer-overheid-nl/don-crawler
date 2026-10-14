package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/developer-overheid-nl/don-crawler/common"
	applog "github.com/developer-overheid-nl/don-crawler/internal/logging"
)

// ForgejoScanner scans Forgejo and Gitea instances (such as code.overheid.nl)
// through their public, unauthenticated API.
type ForgejoScanner struct {
	client *http.Client
}

const (
	defaultForgejoAPIConcurrency = 4
	maxForgejoRateLimitRetries   = 5
	forgejoPageSize              = 50
	forgejoRequestTimeout        = 60 * time.Second
)

var forgejoRateLimitFallbackWait = 15 * time.Second

var errForgejoNotFound = errors.New("forgejo resource not found")

var (
	forgejoAPILimiterOnce sync.Once
	forgejoAPILimiter     chan struct{}
)

type forgejoRepository struct {
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	HTMLURL       string    `json:"html_url"`
	CloneURL      string    `json:"clone_url"`
	DefaultBranch string    `json:"default_branch"`
	Empty         bool      `json:"empty"`
	Fork          bool      `json:"fork"`
	Archived      bool      `json:"archived"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type forgejoCommit struct {
	Created time.Time `json:"created"`
	Commit  struct {
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

func NewForgejoScanner() Scanner {
	return ForgejoScanner{client: &http.Client{Timeout: forgejoRequestTimeout}}
}

func forgejoAPISemaphore() chan struct{} {
	forgejoAPILimiterOnce.Do(func() {
		forgejoAPILimiter = make(chan struct{}, defaultForgejoAPIConcurrency)
	})

	return forgejoAPILimiter
}

// ScanGroupOfRepos sends all repositories of a Forgejo organisation (or user) to
// the repositories channel.
func (scanner ForgejoScanner) ScanGroupOfRepos(
	orgURL url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	applog.Event("forgejo_scanner", "scan_group").WithField("source", orgURL.String()).Debug("Forgejo group scan started")

	owner := strings.Split(strings.Trim(orgURL.Path, "/"), "/")[0]
	if owner == "" {
		return fmt.Errorf("forgejo organisation path is empty for %s", orgURL.String())
	}

	err := scanner.addOwnerRepos(forgejoAPIURL(orgURL, "orgs", owner, "repos"), publisher, repositories)
	if errors.Is(err, errForgejoNotFound) {
		// Not an organisation; try the repositories of a user account.
		err = scanner.addOwnerRepos(forgejoAPIURL(orgURL, "users", owner, "repos"), publisher, repositories)
	}

	if err != nil {
		return fmt.Errorf("can't list Forgejo repositories of '%s': %w", owner, err)
	}

	return nil
}

// ScanRepo sends a single Forgejo repository to the repositories channel.
func (scanner ForgejoScanner) ScanRepo(
	repoURL url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	applog.Event("forgejo_scanner", "scan_repository").
		WithField(applog.FieldRepository, repoURL.String()).
		Debug("Forgejo repository scan started")

	owner, name, err := splitRepoOwnerAndName(repoURL)
	if err != nil {
		return err
	}

	var repo forgejoRepository
	if _, err := scanner.get(context.Background(), forgejoAPIURL(repoURL, "repos", owner, name), &repo); err != nil {
		return fmt.Errorf("can't get Forgejo repository '%s/%s': %w", owner, name, err)
	}

	return addForgejoRepository(&repoURL, repo, publisher, repositories)
}

// LastCommitTimeFromAPI returns the last commit time for a Forgejo repository.
func (scanner ForgejoScanner) LastCommitTimeFromAPI(repoURL url.URL) (time.Time, error) {
	return lastCommitTimeWithRetry("forgejo", func() (time.Time, error) {
		return scanner.lastCommitTime(repoURL)
	})
}

func (scanner ForgejoScanner) lastCommitTime(repoURL url.URL) (time.Time, error) {
	owner, name, err := splitRepoOwnerAndName(repoURL)
	if err != nil {
		return time.Time{}, err
	}

	commitsURL := forgejoAPIURL(repoURL, "repos", owner, name, "commits")
	commitsURL.RawQuery = "limit=1&stat=false&verification=false&files=false"

	var commits []forgejoCommit
	if _, err := scanner.get(context.Background(), commitsURL, &commits); err != nil {
		return time.Time{}, err
	}

	if len(commits) == 0 {
		return time.Time{}, errors.New("no commits found")
	}

	if !commits[0].Commit.Committer.Date.IsZero() {
		return commits[0].Commit.Committer.Date, nil
	}

	if !commits[0].Created.IsZero() {
		return commits[0].Created, nil
	}

	return time.Time{}, errors.New("commit date missing")
}

func (scanner ForgejoScanner) addOwnerRepos(
	listURL url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	listURL.RawQuery = url.Values{
		"limit": {fmt.Sprint(forgejoPageSize)},
		"page":  {"1"},
	}.Encode()

	next := listURL.String()

	for next != "" {
		pageURL, err := url.Parse(next)
		if err != nil {
			return err
		}

		var repos []forgejoRepository

		headers, err := scanner.get(context.Background(), *pageURL, &repos)
		if err != nil {
			return err
		}

		for _, repo := range repos {
			if err := addForgejoRepository(nil, repo, publisher, repositories); err != nil {
				return err
			}
		}

		next = forgejoNextPageURL(headers)
	}

	return nil
}

// get performs a GET request against the Forgejo API, decodes the JSON body into
// out and retries when rate limited.
func (scanner ForgejoScanner) get(ctx context.Context, reqURL url.URL, out any) (http.Header, error) {
	for attempt := 0; ; attempt++ {
		headers, status, err := scanner.doGet(ctx, reqURL, out)
		if err != nil {
			return nil, err
		}

		switch {
		case status >= 200 && status <= 299:
			return headers, nil
		case status == http.StatusNotFound:
			return nil, fmt.Errorf("GET %s: %w", reqURL.String(), errForgejoNotFound)
		case status != http.StatusTooManyRequests:
			return nil, fmt.Errorf("GET %s: unexpected HTTP status %d", reqURL.String(), status)
		}

		reset, _ := common.RateLimitResetFromHeaders(headers)

		if attempt >= maxForgejoRateLimitRetries {
			return nil, RateLimitError{Provider: "Forgejo", Reset: reset}
		}

		wait := time.Until(reset)
		if wait <= 0 {
			wait = forgejoRateLimitFallbackWait
		}

		applog.Event("forgejo_scanner", "wait_for_rate_limit").WithFields(map[string]any{
			applog.FieldURL:    reqURL.String(),
			applog.FieldWaitMS: wait.Milliseconds(),
			"attempt":          attempt + 1,
			"max_attempts":     maxForgejoRateLimitRetries,
		}).Info("Forgejo API rate limited; waiting before retry")

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (scanner ForgejoScanner) doGet(ctx context.Context, reqURL url.URL, out any) (http.Header, int, error) {
	sem := forgejoAPISemaphore()
	sem <- struct{}{}

	defer func() { <-sem }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := scanner.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Drain body so the underlying transport can reuse the TCP connection.
		_, _ = io.Copy(io.Discard, resp.Body)

		return resp.Header, resp.StatusCode, nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("can't parse GET %s response: %w", reqURL.String(), err)
	}

	return resp.Header, resp.StatusCode, nil
}

// forgejoAPIURL builds an API v1 URL on the same host as u.
func forgejoAPIURL(u url.URL, segments ...string) url.URL {
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}

	apiURL, _ := url.Parse(fmt.Sprintf("%s://%s/api/v1/%s", u.Scheme, u.Host, strings.Join(escaped, "/")))

	return *apiURL
}

// forgejoNextPageURL returns the rel="next" URL from a Link header, if any.
func forgejoNextPageURL(headers http.Header) string {
	for _, link := range strings.Split(headers.Get("Link"), ",") {
		parts := strings.Split(link, ";")
		if len(parts) < 2 {
			continue
		}

		for _, param := range parts[1:] {
			if strings.TrimSpace(param) == `rel="next"` {
				return strings.Trim(strings.TrimSpace(parts[0]), "<>")
			}
		}
	}

	return ""
}

// addForgejoRepository sends the Forgejo repository to the repositories channel.
func addForgejoRepository(
	originalURL *url.URL, repo forgejoRepository, publisher common.Publisher, repositories chan common.Repository,
) error {
	if repo.Empty || repo.DefaultBranch == "" {
		applog.Event("forgejo_scanner", "scan_repository").WithFields(map[string]any{
			applog.FieldRepository: repo.FullName,
			applog.FieldReason:     "empty_repository",
		}).Debug("Forgejo repository skipped")

		return nil
	}

	canonicalURL, err := url.Parse(repo.CloneURL)
	if err != nil {
		return fmt.Errorf("failed to get canonical repo URL for %s: %w", repo.HTMLURL, err)
	}

	if originalURL == nil {
		originalURL = canonicalURL
	}

	rawURL, err := url.JoinPath(repo.HTMLURL, "raw", "branch", repo.DefaultBranch, "publiccode.yml")
	if err != nil {
		return err
	}

	repositories <- common.Repository{
		Name:         repo.FullName,
		Title:        repo.Name,
		Description:  repo.Description,
		FileRawURL:   rawURL,
		URL:          *originalURL,
		CanonicalURL: *canonicalURL,
		IsFork:       repo.Fork,
		IsArchived:   repo.Archived,
		GitBranch:    repo.DefaultBranch,
		CreatedAt:    repo.CreatedAt,
		UpdatedAt:    repo.UpdatedAt,
		Publisher:    publisher,
	}

	return nil
}
