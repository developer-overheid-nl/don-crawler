package scanner

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

var ErrLastCommitRateLimited = errors.New("last commit API rate limited")

type RateLimitError struct {
	Provider string
	Reset    time.Time
}

func (e RateLimitError) Error() string {
	if e.Reset.IsZero() {
		return fmt.Sprintf("%s commit API rate limited", e.Provider)
	}

	return fmt.Sprintf("%s commit API rate limited until %s", e.Provider, e.Reset.Format(time.RFC3339))
}

func (e RateLimitError) Is(target error) bool {
	return target == ErrLastCommitRateLimited
}

func splitRepoOwnerAndName(repoURL url.URL) (string, string, error) {
	parts := strings.Split(strings.Trim(repoURL.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("repository path %q does not contain owner and name", repoURL.Path)
	}

	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")

	return owner, repo, nil
}

func lastCommitTimeWithRetry(provider string, fetch func() (time.Time, error)) (time.Time, error) {
	for {
		commitTime, err := fetch()
		if err == nil {
			return commitTime, nil
		}

		var rateLimitErr RateLimitError
		if !errors.As(err, &rateLimitErr) {
			return time.Time{}, err
		}

		if rateLimitErr.Reset.IsZero() {
			return time.Time{}, err
		}

		wait := time.Until(rateLimitErr.Reset)
		if wait <= 0 {
			continue
		}

		waitProvider := provider
		if rateLimitErr.Provider != "" {
			waitProvider = rateLimitErr.Provider
		}

		log.Infof("%s commit API rate limited; waiting until %s", strings.ToLower(waitProvider),
			rateLimitErr.Reset.Format(time.RFC3339))
		time.Sleep(wait)
	}
}
