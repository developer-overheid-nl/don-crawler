package crawler

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
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
	"github.com/italia/publiccode-parser-go/v5"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Crawler is a helper class representing a crawler.
type Crawler struct {
	DryRun bool

	Index        string
	repositories chan common.Repository
	// Sync mutex guard.
	publishersWg   sync.WaitGroup
	repositoriesWg sync.WaitGroup

	gitHubScanner    scanner.Scanner
	gitLabScanner    scanner.Scanner
	bitBucketScanner scanner.Scanner

	apiClient apiclient.APIClient
}

var (
	repoTitleRegexp = regexp.MustCompile(`(?i)<title[^>]*>([^<]*)</title>`)
	repoDescRegexp  = regexp.MustCompile(`(?i)<meta[^>]*name=["']description["'][^>]*content=["']([^"']*)["'][^>]*>`)
)

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

// ScanPublisher scans all the publisher' repositories and sends the ones
// with a valid publiccode.yml to the repositories channel.
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
// and send new data to the API if the publiccode.yml file is valid.
func (c *Crawler) ProcessRepositories(repos chan common.Repository) {
	defer c.repositoriesWg.Done()

	for repository := range repos {
		c.ProcessRepo(repository)
	}
}

// ProcessRepo looks for a publiccode.yml file in a repository, and if found it processes it.
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

	if repository.FileRawURL == "" {
		logEntries = append(logEntries, fmt.Sprintf("[%s] publiccode.yml not found", repository.Name))
		log.Warnf("[%s] publiccode.yml missing, will proceed without it", repository.Name)

		if repository.Title == "" || repository.Description == "" {
			title, desc, metaErr := c.fetchRepoMetadata(repository)
			if metaErr != nil {
				logEntries = append(logEntries, fmt.Sprintf("[%s] failed to fetch repo metadata: %v", repository.Name, metaErr))
			} else {
				if repository.Title == "" && title != "" {
					repository.Title = title
				}

				if repository.Description == "" && desc != "" {
					repository.Description = desc
				}
			}
		}
	} else {
		resp, err := httpclient.GetURL(repository.FileRawURL, repository.Headers)
		statusCode := 0

		if err == nil {
			statusCode = resp.Status.Code
		}

		if statusCode == http.StatusOK && err == nil {
			logEntries = append(
				logEntries,
				fmt.Sprintf(
					"[%s] publiccode.yml found at %s\n",
					repository.CanonicalURL.String(),
					repository.FileRawURL,
				),
			)
		} else {
			logEntries = append(
				logEntries,
				fmt.Sprintf("[%s] Failed to GET publiccode.yml (status: %d)", repository.Name, statusCode),
			)
			log.Warnf("[%s] publiccode.yml not reachable (status: %d), continuing without it", repository.Name, statusCode)
			repository.FileRawURL = ""
		}
	}

	domain := publiccode.Domain{
		Host:        "github.com",
		UseTokenFor: []string{"github.com", "api.github.com", "raw.githubusercontent.com"},
		BasicAuth:   []string{os.Getenv("GITHUB_TOKEN")},
	}

	var parser *publiccode.Parser

	parser, err = publiccode.NewParser(publiccode.ParserConfig{Domain: domain})
	if err != nil {
		logEntries = append(
			logEntries,
			fmt.Sprintf("[%s] can't create a Parser: %s\n", repository.Name, err.Error()),
		)

		return
	}

	var parsed publiccode.PublicCode

	parsed, err = parser.Parse(repository.FileRawURL)

	if c.DryRun {
		log.Infof("[%s]: Skipping other steps (--dry-run)", repository.Name)

		return
	}

	url := repository.CanonicalURL.String()

	publiccodeURL := (*string)(nil)
	if repository.FileRawURL != "" {
		publiccodeURL = &repository.FileRawURL
	}

	if !c.DryRun {
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

		log.Debugf(
			"[%s] posting repository (title=%q desc=%t publiccode=%t)",
			repository.Name,
			deref(repoTitle),
			repoDesc != nil,
			publiccodeURL != nil,
		)

		var (
			lastActivity        = repository.UpdatedAt
			lastActivityFromAPI bool
		)

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
			lastActivity = apiLastActivity
			lastActivityFromAPI = true
		} else if apiErr != nil {
			var rateLimitErr scanner.RateLimitError
			if errors.As(apiErr, &rateLimitErr) {
				log.Infof("[%s] %s", repository.Name, rateLimitErr.Error())
			} else {
				log.Debugf("[%s] last commit via API failed: %v", repository.Name, apiErr)
			}
		}

		// Calculate Repository activity index and vitality. Defaults to 60 days.
		err = git.CloneRepository(repository.URL.Host, repository.Name, parsed.Url().String(), c.Index)
		if err != nil {
			logEntries = append(logEntries, fmt.Sprintf("[%s] error while cloning: %v\n", repository.Name, err))
		}

		activityDays := 60
		if viper.IsSet("ACTIVITY_DAYS") {
			activityDays = viper.GetInt("ACTIVITY_DAYS")
		}

		var activityIndex float64

		activityIndex, _, err = git.CalculateRepoActivity(repository, activityDays)
		if err != nil {
			logEntries = append(
				logEntries, fmt.Sprintf("[%s] error calculating activity index: %v\n", repository.Name, err),
			)
		} else {
			logEntries = append(
				logEntries,
				fmt.Sprintf("[%s] activity index in the last %d days: %f\n", repository.Name, activityDays, activityIndex),
			)
		}

		if !lastActivityFromAPI {
			if last, lastErr := git.LastCommitTime(repository); lastErr == nil {
				lastActivity = last
			} else {
				logEntries = append(
					logEntries,
					fmt.Sprintf("[%s] unable to determine last activity: %v", repository.Name, lastErr),
				)
			}
		}

		if _, err = c.apiClient.PostRepository(
			url,
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

func (c *Crawler) fetchRepoMetadata(repository common.Repository) (string, string, error) {
	repoURL := strings.TrimSuffix(repository.URL.String(), ".git")
	if repoURL == "" {
		repoURL = strings.TrimSuffix(repository.CanonicalURL.String(), ".git")
	}

	if repoURL == "" {
		return "", "", fmt.Errorf("repository URL empty")
	}

	resp, err := httpclient.GetURL(repoURL, repository.Headers)
	if err != nil {
		return "", "", err
	}

	if resp.Status.Code != http.StatusOK {
		return "", "", fmt.Errorf("status %d", resp.Status.Code)
	}

	body := resp.Body

	return extractRepoTitle(body), extractRepoDescription(body), nil
}

func extractRepoTitle(body []byte) string {
	match := repoTitleRegexp.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}

	return strings.TrimSpace(string(match[1]))
}

func extractRepoDescription(body []byte) string {
	match := repoDescRegexp.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}

	return strings.TrimSpace(string(match[1]))
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
