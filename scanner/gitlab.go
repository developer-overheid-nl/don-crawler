package scanner

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/italia/publiccode-crawler/v4/common"
	log "github.com/sirupsen/logrus"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type GitLabScanner struct{}

func NewGitLabScanner() Scanner {
	return GitLabScanner{}
}

// RegisterGitlabAPI register the crawler function for Gitlab API.
func (scanner GitLabScanner) ScanGroupOfRepos(
	url url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	log.Debugf("GitLabScanner.ScanGroupOfRepos(%s)", url.String())

	git, err := newGitlabClient(url)
	if err != nil {
		return err
	}

	if isGitlabGroup(url) {
		groupName := strings.Trim(url.Path, "/")

		group, _, err := git.Groups.GetGroup(groupName, &gitlab.GetGroupOptions{})
		if err != nil {
			return fmt.Errorf("can't get GitLag group '%s': %w", groupName, err)
		}

		if err = addGroupProjects(*group, publisher, repositories, git); err != nil {
			return err
		}
	} else {
		opts := &gitlab.ListProjectsOptions{
			ListOptions: gitlab.ListOptions{Page: 1},
		}

		for {
			projects, res, err := git.Projects.ListProjects(opts)
			if err != nil {
				return err
			}

			for _, prj := range projects {
				if err = addProject(nil, *prj, publisher, repositories); err != nil {
					return err
				}
			}

			if res.NextPage == 0 {
				break
			}

			opts.Page = res.NextPage
		}
	}

	return nil
}

// RegisterSingleGitlabAPI register the crawler function for single Bitbucket API.
func (scanner GitLabScanner) ScanRepo(
	url url.URL, publisher common.Publisher, repositories chan common.Repository,
) error {
	log.Debugf("GitLabScanner.ScanRepo(%s)", url.String())

	git, err := newGitlabClient(url)
	if err != nil {
		return err
	}

	projectName := strings.Trim(url.Path, "/")

	prj, _, err := git.Projects.GetProject(projectName, &gitlab.GetProjectOptions{})
	if err != nil {
		return err
	}

	return addProject(&url, *prj, publisher, repositories)
}

// LastCommitTimeFromAPI returns the last commit time for a GitLab repository.
func (scanner GitLabScanner) LastCommitTimeFromAPI(repoURL url.URL) (time.Time, error) {
	return lastCommitTimeWithRetry("gitlab", func() (time.Time, error) {
		return lastCommitTimeGitLab(repoURL)
	})
}

func lastCommitTimeGitLab(repoURL url.URL) (time.Time, error) {
	projectPath := strings.TrimSuffix(strings.Trim(repoURL.Path, "/"), ".git")
	if projectPath == "" {
		return time.Time{}, fmt.Errorf("gitlab repo path is empty for %s", repoURL.String())
	}

	client, err := newGitlabClient(repoURL)
	if err != nil {
		return time.Time{}, err
	}

	opts := &gitlab.ListCommitsOptions{
		ListOptions: gitlab.ListOptions{Page: 1, PerPage: 1},
	}

	commits, resp, err := client.Commits.ListCommits(projectPath, opts)
	if err != nil {
		if reset, ok := gitlabRateLimitReset(resp, err); ok {
			return time.Time{}, RateLimitError{Provider: "gitlab", Reset: reset}
		}

		return time.Time{}, err
	}

	if len(commits) == 0 {
		return time.Time{}, errors.New("no commits found")
	}

	if commits[0].CommittedDate != nil {
		return *commits[0].CommittedDate, nil
	}

	if commits[0].CreatedAt != nil {
		return *commits[0].CreatedAt, nil
	}

	return time.Time{}, errors.New("commit date missing")
}

// isGitlabGroup returns true if the API URL points to a group.
func isGitlabGroup(u url.URL) bool {
	return (
	// Always assume it's a group if the projects are hosted on gitlab.com,
	// because we only want to support groups (ie. not repos belonging to a user)
	strings.ToLower(u.Hostname()) == "gitlab.com" ||
		// Assume an on-premise GitLab's URL is a group if the path is not the root
		// path (/) or empty
		len(u.Path) > 1)
}

func gitlabTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}

	return *t
}

func gitlabUpdatedAt(project gitlab.Project) time.Time {
	if project.LastActivityAt != nil {
		return *project.LastActivityAt
	}

	return gitlabTime(project.UpdatedAt)
}

func newGitlabClient(u url.URL) (*gitlab.Client, error) {
	token := os.Getenv("GITLAB_TOKEN")

	if u.Scheme == "" || u.Host == "" {
		return gitlab.NewClient(token)
	}

	base := fmt.Sprintf("%s://%s/api/v4", u.Scheme, u.Host)

	return gitlab.NewClient(token, gitlab.WithBaseURL(base))
}

func gitlabRateLimitReset(resp *gitlab.Response, err error) (time.Time, bool) {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		reset, ok := rateLimitResetFromHeaders(resp.Header)
		return reset, ok
	}

	var errResp *gitlab.ErrorResponse
	if errors.As(err, &errResp) && errResp.HasStatusCode(http.StatusTooManyRequests) {
		reset, ok := rateLimitResetFromHeaders(errResp.Response.Header)
		return reset, ok
	}

	return time.Time{}, false
}

// generateGitlabRawURL returns the file Gitlab specific file raw url.
func generateGitlabRawURL(baseURL, defaultBranch string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	u.Path = path.Join(u.Path, "raw", defaultBranch, "publiccode.yml")

	return u.String(), err
}

// addGroupProjects sends all the projects in a GitLab group, including all subgroups, to
// the repositories channel.
func addGroupProjects(
	group gitlab.Group, publisher common.Publisher, repositories chan common.Repository, client *gitlab.Client,
) error {
	opts := &gitlab.ListGroupProjectsOptions{
		ListOptions: gitlab.ListOptions{Page: 1},
	}

	for {
		projects, res, err := client.Groups.ListGroupProjects(group.ID, opts)
		if err != nil {
			return err
		}

		for _, prj := range projects {
			err = addProject(nil, *prj, publisher, repositories)
			if err != nil {
				return err
			}
		}

		if res.NextPage == 0 {
			break
		}

		opts.Page = res.NextPage
	}

	dgOpts := &gitlab.ListDescendantGroupsOptions{
		ListOptions: gitlab.ListOptions{Page: 1},
	}

	for {
		groups, res, err := client.Groups.ListDescendantGroups(group.ID, dgOpts)
		if err != nil {
			return err
		}

		for _, g := range groups {
			err = addGroupProjects(*g, publisher, repositories, client)
			if err != nil {
				return err
			}
		}

		if res.NextPage == 0 {
			break
		}

		dgOpts.Page = res.NextPage
	}

	return nil
}

// addGroupProjects sends the GitLab project the repositories channel.
func addProject(
	originalURL *url.URL, project gitlab.Project, publisher common.Publisher, repositories chan common.Repository,
) error {
	// Join file raw URL string.
	rawURL, err := generateGitlabRawURL(project.WebURL, project.DefaultBranch)
	if err != nil {
		return err
	}

	if project.DefaultBranch != "" {
		canonicalURL, err := url.Parse(project.HTTPURLToRepo)
		if err != nil {
			return fmt.Errorf("failed to get canonical repo URL for %s: %w", project.WebURL, err)
		}

		if originalURL == nil {
			originalURL = canonicalURL
		}

		repositories <- common.Repository{
			Name:         project.PathWithNamespace,
			Title:        project.Name,
			Description:  project.Description,
			FileRawURL:   rawURL,
			URL:          *originalURL,
			CanonicalURL: *canonicalURL,
			GitBranch:    project.DefaultBranch,
			CreatedAt:    gitlabTime(project.CreatedAt),
			UpdatedAt:    gitlabUpdatedAt(project),
			Publisher:    publisher,
		}
	}

	return nil
}
