package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/alranel/go-vcsurl/v2"
	"github.com/developer-overheid-nl/don-crawler/apiclient"
	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/developer-overheid-nl/don-crawler/git"
	applog "github.com/developer-overheid-nl/don-crawler/internal/logging"
	"github.com/developer-overheid-nl/don-crawler/scanner"
	"github.com/spf13/viper"
)

const (
	publiccodeRequestTimeout        = 60 * time.Second
	publiccodeRateLimitMaxRetries   = 6
	publiccodeRateLimitFallbackWait = 15 * time.Second
	publiccodeRateLimitMaxWait      = 5 * time.Minute
	repositoryWorkerCount           = 1
	publisherWorkerCount            = 2
	repositoryChannelSize           = 100
)

var publiccodeHTTPClient = &http.Client{Timeout: publiccodeRequestTimeout}

// Crawler is a helper class representing a crawler.
type Crawler struct {
	DryRun bool

	Index        string
	repositories chan common.Repository
	repoLocks    repoLockMap
	// Sync mutex guard.
	publishersWg   sync.WaitGroup
	repositoriesWg sync.WaitGroup

	gitHubScanner    scanner.Scanner
	gitLabScanner    scanner.Scanner
	bitBucketScanner scanner.Scanner

	apiClient apiclient.APIClient
}

// repoLockMap provides per-repository locks for git operations.
type repoLockMap struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (r *repoLockMap) lock(key string) func() {
	r.mu.Lock()

	if r.locks == nil {
		r.locks = make(map[string]*sync.Mutex)
	}

	lock := r.locks[key]

	if lock == nil {
		lock = &sync.Mutex{}
		r.locks[key] = lock
	}

	r.mu.Unlock()

	lock.Lock()

	return lock.Unlock
}

// NewCrawler initializes a new Crawler object and connects to Elasticsearch (if dryRun == false).
func NewCrawler(dryRun bool) *Crawler {
	var c Crawler

	c.DryRun = dryRun

	datadir := viper.GetString("DATADIR")
	if err := os.MkdirAll(datadir, 0o744); err != nil {
		applog.Event("crawler", "create_data_directory").WithFields(map[string]any{
			"data_directory":  datadir,
			applog.FieldError: err,
		}).Fatal("Data directory could not be created")
	}

	// Initiate a channel of repositories.
	c.repositories = make(chan common.Repository, repositoryChannelSize)

	c.gitHubScanner = scanner.NewGitHubScanner()
	c.gitLabScanner = scanner.NewGitLabScanner()
	c.bitBucketScanner = scanner.NewBitBucketScanner()

	c.apiClient = apiclient.NewClient()

	return &c
}

// CrawlSoftwareByAPIURL crawls a single software.
func (c *Crawler) CrawlSoftwareByID(_ string, _ common.Publisher) error {
	// var id string

	// softwareURL, err := url.Parse(software)
	// if err != nil {
	// 	id = software
	// } else {
	// 	id = path.Base(softwareURL.Path)
	// }

	// s, err := c.apiClient.GetSoftware(id)
	// if err != nil {
	// 	return err
	// }

	// s.URL = strings.TrimSuffix(s.URL, ".git")

	// repoURL, err := url.Parse(s.URL)
	// if err != nil {
	// 	return err
	// }

	// log.Infof("Processing repository: %s", softwareURL.String())

	// switch {
	// case vcsurl.IsGitHub(repoURL):
	// 	err = c.gitHubScanner.ScanRepo(*repoURL, publisher, c.repositories)
	// case vcsurl.IsBitBucket(repoURL):
	// 	err = c.bitBucketScanner.ScanRepo(*repoURL, publisher, c.repositories)
	// case vcsurl.IsGitLab(repoURL):
	// 	err = c.gitLabScanner.ScanRepo(*repoURL, publisher, c.repositories)
	// default:
	// 	err = fmt.Errorf(
	// 		"publisher %s: unsupported code hosting platform for %s",
	// 		publisher.Name,
	// 		repoURL.String(),
	// 	)
	// }

	// if err != nil {
	// 	return err
	// }

	// close(c.repositories)

	// return c.crawl()
	return nil
}

// CrawlPublishers processes a list of publishers.
func (c *Crawler) CrawlPublishers(publishers []common.Publisher) error {
	reposNum := 0
	for _, publisher := range publishers {
		reposNum += len(publisher.Repositories)
	}

	applog.Event("crawler", "scan_publishers").WithFields(map[string]any{
		"publisher_count":  len(publishers),
		"repository_count": reposNum,
	}).Info("Publisher scan started")

	publisherJobs := make(chan common.Publisher)

	for i := range publisherWorkerCount {
		c.publishersWg.Add(1)

		go func(id int) {
			defer c.publishersWg.Done()

			applog.Event("crawler", "start_publisher_worker").WithField("worker_id", id).Debug("Publisher worker started")

			for publisher := range publisherJobs {
				c.ScanPublisher(publisher)
			}
		}(i)
	}

	go func() {
		for _, publisher := range publishers {
			publisherJobs <- publisher
		}

		close(publisherJobs)
	}()

	// Close the repositories channel when all the publisher workers are done.
	go func() {
		c.publishersWg.Wait()
		close(c.repositories)
	}()

	return c.crawl()
}

// ScanPublisher scans all the publisher' repositories and sends any repository
// with a publiccode.yml to the repositories channel.
func (c *Crawler) ScanPublisher(publisher common.Publisher) {
	applog.Event("crawler", "scan_publisher").WithFields(map[string]any{
		applog.FieldPublisher: publisher.Name,
		"publisher_id":        publisher.ID,
	}).Info("Publisher scan started")

	var err error

	orgURL := (url.URL)(publisher.Organization)

	switch {
	case vcsurl.IsGitHub(&orgURL):
		err = c.gitHubScanner.ScanGroupOfRepos(orgURL, publisher, c.repositories)
	case vcsurl.IsBitBucket(&orgURL):
		err = c.bitBucketScanner.ScanGroupOfRepos(orgURL, publisher, c.repositories)
	case vcsurl.IsGitLab(&orgURL):
		err = c.gitLabScanner.ScanGroupOfRepos(orgURL, publisher, c.repositories)
	default:
		err = fmt.Errorf(
			"publisher %s: unsupported code hosting platform for %s",
			publisher.Name,
			orgURL.String(),
		)
	}

	if err != nil {
		if errors.Is(err, scanner.ErrPubliccodeNotFound) || errors.Is(err, scanner.ErrRepositorySkipped) {
			applog.Event("crawler", "scan_publisher").WithFields(map[string]any{
				applog.FieldPublisher: publisher.Name,
				"source":              orgURL.String(),
				applog.FieldError:     err,
			}).Debug("Publisher scan skipped")
		} else {
			applog.Event("crawler", "scan_publisher").WithFields(map[string]any{
				applog.FieldPublisher: publisher.Name,
				"source":              orgURL.String(),
				applog.FieldError:     err,
			}).Error("Publisher scan failed")
		}
	}

	for _, u := range publisher.Repositories {
		repoURL := (url.URL)(u)

		switch {
		case vcsurl.IsGitHub(&repoURL):
			err = c.gitHubScanner.ScanRepo(repoURL, publisher, c.repositories)
		case vcsurl.IsBitBucket(&repoURL):
			err = c.bitBucketScanner.ScanRepo(repoURL, publisher, c.repositories)
		case vcsurl.IsGitLab(&repoURL):
			err = c.gitLabScanner.ScanRepo(repoURL, publisher, c.repositories)
		default:
			err = fmt.Errorf(
				"publisher %s: unsupported code hosting platform for %s",
				publisher.Name,
				u.String(),
			)
		}

		if err != nil {
			if errors.Is(err, scanner.ErrPubliccodeNotFound) || errors.Is(err, scanner.ErrRepositorySkipped) {
				applog.Event("crawler", "scan_repository").WithFields(map[string]any{
					applog.FieldPublisher:  publisher.Name,
					applog.FieldRepository: repoURL.String(),
					applog.FieldError:      err,
				}).Debug("Repository scan skipped")
			} else {
				applog.Event("crawler", "scan_repository").WithFields(map[string]any{
					applog.FieldPublisher:  publisher.Name,
					applog.FieldRepository: repoURL.String(),
					applog.FieldError:      err,
				}).Error("Repository scan failed")
			}
		}
	}
}

// ProcessRepositories process the repositories channel, check the repo's publiccode.yml
// and send new data to the API.
func (c *Crawler) ProcessRepositories(repos chan common.Repository) {
	defer c.repositoriesWg.Done()

	for repository := range repos {
		c.ProcessRepo(repository)
	}
}

// ProcessRepo looks for a publiccode.yml file in a repository, and if found it records the link.
func (c *Crawler) ProcessRepo(repository common.Repository) {
	c.ensurePubliccodeFile(context.Background(), &repository)
	hasPubliccode := repository.FileRawURL != ""

	if c.DryRun {
		applog.Event("crawler", "process_repository").WithFields(map[string]any{
			applog.FieldRepository: repository.Name,
			"dry_run":              true,
		}).Info("Repository processing stopped after scan")

		return
	}

	cloneURL := repository.CanonicalURL.String()

	cloneErr := c.cloneAndLogActivity(repository, cloneURL)

	if viper.GetBool("CLEANUP_GIT_CLONES") {
		defer func() {
			if err := git.RemoveRepository(repository.URL.Host, repository.Name); err != nil {
				applog.Event("crawler", "remove_clone").WithFields(map[string]any{
					applog.FieldRepository: repository.Name,
					applog.FieldError:      err,
				}).Warn("Local repository clone could not be removed")
			}
		}()
	}

	if !hasPubliccode {
		if repository.Description == "" && cloneErr == nil {
			readmeContents, readmeErr := git.ReadReadme(repository)
			if readmeErr != nil {
				if !errors.Is(readmeErr, git.ErrReadmeNotFound) {
					applog.Event("crawler", "read_readme").WithFields(map[string]any{
						applog.FieldRepository: repository.Name,
						applog.FieldError:      readmeErr,
					}).Warn("Repository README could not be read")
				}
			} else {
				repository.Description = descriptionFromReadme(readmeContents)
			}
		}

		if repository.Title == "" {
			repository.Title = titleFromRepositoryName(repository)
		}
	}

	publiccodeURL := repositoryPubliccodeURL(repository)

	var repoTitle, repoDesc *string
	if hasPubliccode {
		repoTitle = nil
		repoDesc = nil
	} else {
		repoTitle, repoDesc = repoPostDetails(repository)
	}

	applog.Event("crawler", "post_repository").WithFields(map[string]any{
		applog.FieldRepository: repository.Name,
		"repository_title":     deref(repoTitle),
		"description_present":  repoDesc != nil,
		"publiccode_present":   publiccodeURL != nil,
		"archived":             repository.IsArchived,
	}).Debug("Repository prepared for API submission")

	lastActivity := c.lastActivityFromGit(repository, cloneErr)

	if _, err := c.apiClient.PostRepository(
		repository.CanonicalURL.String(),
		repoTitle,
		repoDesc,
		publiccodeURL,
		&repository.IsFork,
		&repository.IsArchived,
		orgURI(repository.Publisher),
		repository.CreatedAt,
		time.Now(),
		lastActivity,
	); err != nil {
		logPostRepositoryError(repository.Name, err)
	}
}

func logPostRepositoryError(repositoryName string, err error) {
	applog.Event("crawler", "post_repository").WithFields(map[string]any{
		applog.FieldRepository: repositoryName,
		applog.FieldError:      err,
	}).Errorf("[%s] PostRepository failed: %v", repositoryName, err)
}

func publiccodeGetStatus(ctx context.Context, resourceURL string, headers map[string]string) (int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil)
	if err != nil {
		return 0, nil, err
	}

	for k, v := range headers {
		if strings.TrimSpace(k) == "" || v == "" {
			continue
		}

		req.Header.Set(k, v)
	}

	resp, err := publiccodeHTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	// Drain body so the underlying transport can reuse the TCP connection.
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, resp.Header, nil
}

func rateLimitWaitFromHeaders(headers http.Header) time.Duration {
	if headers == nil {
		return publiccodeRateLimitFallbackWait
	}

	if reset, ok := common.RateLimitResetFromHeaders(headers); ok {
		wait := time.Until(reset)
		if wait > publiccodeRateLimitMaxWait {
			return publiccodeRateLimitMaxWait
		}

		if wait > 0 {
			return wait
		}
	}

	return publiccodeRateLimitFallbackWait
}

func isRateLimitedStatus(statusCode int, headers http.Header) bool {
	if statusCode == http.StatusTooManyRequests {
		return true
	}

	if statusCode != http.StatusForbidden || headers == nil {
		return false
	}

	if headers.Get("Retry-After") != "" {
		return true
	}

	if _, ok := common.RateLimitResetFromHeaders(headers); ok {
		return true
	}

	return headers.Get("X-RateLimit-Remaining") == "0"
}

func publiccodeGetStatusWithRetry(ctx context.Context, resourceURL string, headers map[string]string) (int, error) {
	for attempts := 0; ; attempts++ {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}

		statusCode, responseHeaders, err := publiccodeGetStatus(ctx, resourceURL, headers)
		if err != nil {
			return 0, err
		}

		if !isRateLimitedStatus(statusCode, responseHeaders) {
			return statusCode, nil
		}

		if attempts >= publiccodeRateLimitMaxRetries {
			return statusCode, fmt.Errorf("publiccode.yml request remained rate limited after %d attempts", attempts+1)
		}

		wait := rateLimitWaitFromHeaders(responseHeaders)
		applog.Event("crawler", "wait_for_publiccode_rate_limit").WithFields(map[string]any{
			applog.FieldURL:        resourceURL,
			applog.FieldStatusCode: statusCode,
			"wait_ms":              wait.Milliseconds(),
			"attempt":              attempts + 1,
		}).Info("publiccode.yml request rate limited; waiting before retry")

		select {
		case <-ctx.Done():
			return statusCode, ctx.Err()
		case <-time.After(wait):
			// Continue to next retry.
		}
	}
}

func (c *Crawler) ensurePubliccodeFile(ctx context.Context, repository *common.Repository) {
	if repository.FileRawURL == "" {
		applog.Event("crawler", "check_publiccode").
			WithField(applog.FieldRepository, repository.Name).
			Debugf("[%s] publiccode.yml not found", repository.Name)

		return
	}

	statusCode, err := publiccodeGetStatusWithRetry(ctx, repository.FileRawURL, repository.Headers)

	if statusCode == http.StatusOK && err == nil {
		applog.Event("crawler", "check_publiccode").WithFields(map[string]any{
			applog.FieldRepository: repository.Name,
			"publiccode_url":       repository.FileRawURL,
			applog.FieldStatusCode: statusCode,
		}).Debugf("[%s] publiccode.yml found at %s", repository.Name, repository.FileRawURL)

		return
	}

	if err != nil {
		applog.Event("crawler", "check_publiccode").WithFields(map[string]any{
			applog.FieldRepository: repository.Name,
			applog.FieldURL:        repository.FileRawURL,
			applog.FieldError:      err,
		}).Warnf("[%s] publiccode.yml request failed: %v", repository.Name, err)
		repository.FileRawURL = ""

		return
	}

	if statusCode == http.StatusNotFound {
		applog.Event("crawler", "check_publiccode").WithFields(map[string]any{
			applog.FieldRepository: repository.Name,
			applog.FieldURL:        repository.FileRawURL,
			applog.FieldStatusCode: statusCode,
		}).Debugf("[%s] publiccode.yml not reachable (status: %d)", repository.Name, statusCode)
	} else {
		applog.Event("crawler", "check_publiccode").WithFields(map[string]any{
			applog.FieldRepository: repository.Name,
			applog.FieldURL:        repository.FileRawURL,
			applog.FieldStatusCode: statusCode,
		}).Warnf(
			"[%s] publiccode.yml not reachable (status: %d), continuing without it",
			repository.Name,
			statusCode,
		)
	}

	repository.FileRawURL = ""
}

func titleFromRepositoryName(repository common.Repository) string {
	if repository.Name == "" {
		return ""
	}

	return path.Base(repository.Name)
}

func (c *Crawler) lastActivityFromAPI(repository common.Repository) (time.Time, bool) {
	lastActivity := repository.UpdatedAt

	var apiLastActivity time.Time

	var apiErr error

	switch {
	case vcsurl.IsGitHub(&repository.CanonicalURL):
		apiLastActivity, apiErr = c.gitHubScanner.LastCommitTimeFromAPI(repository.CanonicalURL)
	case vcsurl.IsBitBucket(&repository.CanonicalURL):
		apiLastActivity, apiErr = c.bitBucketScanner.LastCommitTimeFromAPI(repository.CanonicalURL)
	case vcsurl.IsGitLab(&repository.CanonicalURL):
		apiLastActivity, apiErr = c.gitLabScanner.LastCommitTimeFromAPI(repository.CanonicalURL)
	default:
		apiErr = fmt.Errorf("unsupported repository host %s", repository.CanonicalURL.Host)
	}

	if apiErr == nil && !apiLastActivity.IsZero() {
		return apiLastActivity, true
	}

	if apiErr != nil {
		var rateLimitErr scanner.RateLimitError
		if errors.As(apiErr, &rateLimitErr) {
			applog.Event("crawler", "determine_last_activity").WithFields(map[string]any{
				applog.FieldRepository: repository.Name,
				"provider":             rateLimitErr.Provider,
				applog.FieldResetTime:  rateLimitErr.Reset,
				applog.FieldError:      apiErr,
			}).Infof("[%s] %s", repository.Name, rateLimitErr.Error())
		} else {
			applog.Event("crawler", "determine_last_activity").WithFields(map[string]any{
				applog.FieldRepository: repository.Name,
				applog.FieldError:      apiErr,
			}).Debugf("[%s] last commit via API failed: %v", repository.Name, apiErr)
		}
	}

	return lastActivity, false
}

func (c *Crawler) cloneAndLogActivity(
	repository common.Repository,
	cloneURL string,
) error {
	// Calculate Repository activity index and vitality. Defaults to 60 days.
	if cloneURL == "" {
		applog.Event("crawler", "clone_repository").
			WithField(applog.FieldRepository, repository.Name).
			Warnf("[%s] unable to determine clone URL", repository.Name)

		return errors.New("clone URL empty")
	}

	unlock := c.repoLocks.lock(repoLockKey(repository))

	applog.Event("crawler", "clone_repository").WithFields(map[string]any{
		applog.FieldRepository: repository.Name,
		"clone_url":            cloneURL,
	}).Debug("Repository clone started")
	err := git.CloneRepository(repository.URL.Host, repository.Name, cloneURL, repository.GitBranch)

	unlock()

	if err != nil {
		applog.Event("crawler", "clone_repository").WithFields(map[string]any{
			applog.FieldRepository: repository.Name,
			"clone_url":            cloneURL,
			applog.FieldError:      err,
		}).Warn("Repository clone failed")

		return err
	}

	activityDays := activityDays()

	applog.Event("crawler", "calculate_activity").WithFields(map[string]any{
		applog.FieldRepository:   repository.Name,
		applog.FieldActivityDays: activityDays,
	}).Debug("Repository activity calculation started")

	activityIndex, _, err := git.CalculateRepoActivity(repository, activityDays)
	if err != nil {
		applog.Event("crawler", "calculate_activity").WithFields(map[string]any{
			applog.FieldRepository:   repository.Name,
			applog.FieldActivityDays: activityDays,
			applog.FieldError:        err,
		}).Warn("Repository activity index could not be calculated")
	} else {
		applog.Event("crawler", "calculate_activity").WithFields(map[string]any{
			applog.FieldRepository:   repository.Name,
			applog.FieldActivityDays: activityDays,
			"activity_index":         activityIndex,
		}).Debug("Repository activity index calculated")
	}

	return err
}

func (c *Crawler) lastActivityFromGit(
	repository common.Repository,
	cloneErr error,
) time.Time {
	lastActivity := repository.UpdatedAt

	last, lastErr := git.LastCommitTime(repository)
	if lastErr == nil {
		return last
	}

	if cloneErr != nil {
		apiLast, ok := c.lastActivityFromAPI(repository)

		if ok {
			return apiLast
		}
	}

	applog.Event("crawler", "determine_last_activity").WithFields(map[string]any{
		applog.FieldRepository: repository.Name,
		applog.FieldError:      lastErr,
	}).Warnf(
		"[%s] unable to determine last activity: %v; falling back to repository updated timestamp",
		repository.Name,
		lastErr,
	)

	return lastActivity
}

func repositoryPubliccodeURL(repository common.Repository) *string {
	if repository.FileRawURL == "" {
		return nil
	}

	return &repository.FileRawURL
}

func repoPostDetails(repository common.Repository) (*string, *string) {
	title := repository.Title
	if title == "" {
		title = repository.Name
	}

	desc := ensureDescription(repository)

	repoTitle := &title
	if title == "" {
		repoTitle = nil
	}

	repoDesc := &desc

	return repoTitle, repoDesc
}

func repoLockKey(repository common.Repository) string {
	if repository.Name == "" {
		return repository.URL.Host
	}

	parts := strings.Split(repository.Name, "/")

	if len(parts) < 2 {
		return repository.URL.Host + "/" + repository.Name
	}

	return fmt.Sprintf("%s/%s/%s", repository.URL.Host, parts[0], parts[1])
}

func activityDays() int {
	if viper.IsSet("ACTIVITY_DAYS") {
		return viper.GetInt("ACTIVITY_DAYS")
	}

	return 60
}

func (c *Crawler) crawl() error {
	reposChan := make(chan common.Repository)

	defer c.publishersWg.Wait()

	applog.Event("crawler", "start_repository_workers").
		WithField("worker_count", repositoryWorkerCount).
		Debug("Repository workers configured")

	// Process the repositories in order to retrieve the files.
	for i := range repositoryWorkerCount {
		c.repositoriesWg.Add(1)

		go func(id int) {
			applog.Event("crawler", "start_repository_worker").WithField("worker_id", id).Debug("Repository worker started")
			c.ProcessRepositories(reposChan)
		}(i)
	}

	for repo := range c.repositories {
		reposChan <- repo
	}

	close(reposChan)
	c.repositoriesWg.Wait()

	applog.Event("crawler", "complete").Info("Crawler run completed")

	return nil
}

func descriptionFromReadme(contents string) string {
	contents = strings.ReplaceAll(contents, "\r\n", "\n")
	lines := strings.Split(contents, "\n")

	paragraph := make([]string, len(lines))

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			if len(paragraph) > 0 {
				break
			}

			continue
		}

		if len(paragraph) == 0 && isReadmeSkippableLine(trimmed) {
			continue
		}

		paragraph[i] = trimmed
	}

	return strings.Join(paragraph, " ")
}

func isReadmeSkippableLine(line string) bool {
	lower := strings.ToLower(line)

	if strings.HasPrefix(line, "#") {
		return true
	}

	if strings.HasPrefix(lower, "<img") || strings.HasPrefix(lower, "<a") {
		return true
	}

	if strings.HasPrefix(line, "![") || strings.HasPrefix(line, "[!") {
		return true
	}

	return false
}

func ensureDescription(repository common.Repository) string {
	if repository.Description != "" {
		return repository.Description
	}

	if repository.Title != "" {
		return repository.Title
	}

	if repository.Name != "" {
		return repository.Name
	}

	return "No description provided"
}

func deref(v *string) string {
	if v == nil {
		return ""
	}

	return *v
}

func orgURI(publisher common.Publisher) string {
	if publisher.OrganisationURL != "" {
		return publisher.OrganisationURL
	}

	return publisher.Organization.String()
}
