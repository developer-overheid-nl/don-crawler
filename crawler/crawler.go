package crawler

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/alranel/go-vcsurl/v2"
	httpclient "github.com/italia/httpclient-lib-go"
	"github.com/italia/publiccode-crawler/v4/apiclient"
	"github.com/italia/publiccode-crawler/v4/common"
	"github.com/italia/publiccode-crawler/v4/git"
	"github.com/italia/publiccode-crawler/v4/scanner"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

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

	const channelSize = 1000

	c.DryRun = dryRun

	datadir := viper.GetString("DATADIR")
	if err := os.MkdirAll(datadir, 0o744); err != nil {
		log.Fatalf("can't create data directory (%s): %s", datadir, err.Error())
	}

	// Initiate a channel of repositories.
	c.repositories = make(chan common.Repository, channelSize)

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

	log.Infof("Scanning %d publishers (%d repositories)", len(publishers), reposNum)

	// Process every item in publishers.
	for _, publisher := range publishers {
		c.publishersWg.Add(1)

		go c.ScanPublisher(publisher)
	}

	// Close the repositories channel when all the publisher goroutines are done
	go func() {
		c.publishersWg.Wait()
		close(c.repositories)
	}()

	return c.crawl()
}

// ScanPublisher scans all the publisher' repositories and sends any repository
// with a publiccode.yml to the repositories channel.
func (c *Crawler) ScanPublisher(publisher common.Publisher) {
	log.Infof("Processing publisher: %s", publisher.Name)

	defer c.publishersWg.Done()

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
		if errors.Is(err, scanner.ErrPubliccodeNotFound) {
			log.Warnf("[%s] %s", orgURL.String(), err.Error())
		} else {
			log.Error(err)
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
			if errors.Is(err, scanner.ErrPubliccodeNotFound) {
				log.Warnf("[%s] %s", repoURL.String(), err.Error())
			} else {
				log.Error(err)
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
	var (
		logEntries []string
		err        error
	)

	defer func() {
		for _, e := range logEntries {
			log.Info(e)
		}
	}()

	c.ensurePubliccodeFile(&repository, &logEntries)
	hasPubliccode := repository.FileRawURL != ""

	if c.DryRun {
		log.Infof("[%s]: Skipping other steps (--dry-run)", repository.Name)

		return
	}

	cloneURL := repository.CanonicalURL.String()

	cloneErr := c.cloneAndLogActivity(repository, cloneURL, &logEntries)

	if !hasPubliccode {
		if repository.Description == "" && cloneErr == nil {
			readmeContents, readmeErr := git.ReadReadme(repository)
			if readmeErr != nil {
				if !errors.Is(readmeErr, git.ErrReadmeNotFound) {
					logEntries = append(
						logEntries,
						fmt.Sprintf("[%s] failed to read README: %v", repository.Name, readmeErr),
					)
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

	log.Debugf(
		"[%s] posting repository (title=%q desc=%t publiccode=%t)",
		repository.Name,
		deref(repoTitle),
		repoDesc != nil,
		publiccodeURL != nil,
	)

	lastActivity := c.lastActivityFromGit(repository, cloneErr, &logEntries)

	if _, err = c.apiClient.PostRepository(
		repository.CanonicalURL.String(),
		repoTitle,
		repoDesc,
		publiccodeURL,
		orgURI(repository.Publisher),
		repository.CreatedAt,
		time.Now(),
		lastActivity,
	); err != nil {
		logEntries = append(logEntries, fmt.Sprintf("[%s]: %s", repository.Name, err.Error()))
		log.Errorf("[%s] PostRepository failed: %v", repository.Name, err)
	}
}

func (c *Crawler) ensurePubliccodeFile(repository *common.Repository, logEntries *[]string) {
	if repository.FileRawURL == "" {
		*logEntries = append(*logEntries, fmt.Sprintf("[%s] publiccode.yml not found", repository.Name))
		log.Warnf("[%s] publiccode.yml missing, will proceed without it", repository.Name)

		return
	}

	resp, err := httpclient.GetURL(repository.FileRawURL, repository.Headers)
	statusCode := 0

	if err == nil {
		statusCode = resp.Status.Code
	}

	if statusCode == http.StatusOK && err == nil {
		*logEntries = append(
			*logEntries,
			fmt.Sprintf(
				"[%s] publiccode.yml found at %s\n",
				repository.CanonicalURL.String(),
				repository.FileRawURL,
			),
		)

		return
	}

	*logEntries = append(
		*logEntries,
		fmt.Sprintf("[%s] Failed to GET publiccode.yml (status: %d)", repository.Name, statusCode),
	)
	log.Warnf("[%s] publiccode.yml not reachable (status: %d), continuing without it", repository.Name, statusCode)
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
			log.Infof("[%s] %s", repository.Name, rateLimitErr.Error())
		} else {
			log.Debugf("[%s] last commit via API failed: %v", repository.Name, apiErr)
		}
	}

	return lastActivity, false
}

func (c *Crawler) cloneAndLogActivity(
	repository common.Repository,
	cloneURL string,
	logEntries *[]string,
) error {
	// Calculate Repository activity index and vitality. Defaults to 60 days.
	if cloneURL == "" {
		*logEntries = append(*logEntries, fmt.Sprintf("[%s] unable to determine clone URL\n", repository.Name))

		return errors.New("clone URL empty")
	}

	unlock := c.repoLocks.lock(repoLockKey(repository))

	err := git.CloneRepository(repository.URL.Host, repository.Name, cloneURL, c.Index)

	unlock()

	if err != nil {
		*logEntries = append(*logEntries, fmt.Sprintf("[%s] error while cloning: %v\n", repository.Name, err))
	}

	activityDays := activityDays()

	activityIndex, _, err := git.CalculateRepoActivity(repository, activityDays)
	if err != nil {
		*logEntries = append(
			*logEntries,
			fmt.Sprintf("[%s] error calculating activity index: %v\n", repository.Name, err),
		)
	} else {
		*logEntries = append(
			*logEntries,
			fmt.Sprintf("[%s] activity index in the last %d days: %f\n", repository.Name, activityDays, activityIndex),
		)
	}

	return err
}

func (c *Crawler) lastActivityFromGit(
	repository common.Repository,
	cloneErr error,
	logEntries *[]string,
) time.Time {
	lastActivity := repository.UpdatedAt

	last, lastErr := git.LastCommitTime(repository)
	if lastErr == nil {
		return last
	}

	*logEntries = append(
		*logEntries,
		fmt.Sprintf("[%s] unable to determine last activity: %v", repository.Name, lastErr),
	)

	if cloneErr != nil {
		apiLast, ok := c.lastActivityFromAPI(repository)

		if ok {
			return apiLast
		}
	}

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

	// Get cpus number
	numCPUs := runtime.NumCPU()

	workerCount := int(math.Ceil(float64(numCPUs) * 0.7))
	if workerCount < 1 {
		workerCount = 1
	}

	log.Debugf("CPUs #: %d (workers: %d)", numCPUs, workerCount)

	// Process the repositories in order to retrieve the files.
	for i := range workerCount {
		c.repositoriesWg.Add(1)

		go func(id int) {
			log.Debugf("Starting ProcessRepositories() goroutine (#%d)", id)
			c.ProcessRepositories(reposChan)
		}(i)
	}

	for repo := range c.repositories {
		reposChan <- repo
	}

	close(reposChan)
	c.repositoriesWg.Wait()

	log.Info("Crawler run completed")

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
