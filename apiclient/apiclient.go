package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/italia/publiccode-crawler/v4/common"
	internalUrl "github.com/italia/publiccode-crawler/v4/internal"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type APIClient struct {
	baseURL         string
	retryableClient *http.Client
	token           string
	xAPIKey         string
}

type GitOrganisation struct {
	ID           string           `json:"id"`
	Organisation *Organisation    `json:"organisation"`
	CodeHosting  []CodeHostingDTO `json:"codeHosting"`
}

type Organisation struct {
	URI   string `json:"uri"`
	Label string `json:"label"`
}

type CodeHostingDTO struct {
	URL   string `json:"url"`
	Group *bool  `json:"group"`
}

type Repository struct {
	ID            string  `json:"id"`
	RepositoryURL string  `json:"repositoryUrl"`
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	PublicCodeURL *string `json:"publicCodeUrl"`
}

type repositoryRequest struct {
	RepositoryURL    string  `json:"repositoryUrl"`
	Name             *string `json:"name,omitempty"`
	Description      *string `json:"description,omitempty"`
	PubliccodeYmlURL *string `json:"publiccodeYmlUrl,omitempty"`
	Active           bool    `json:"active"`
}

func NewClient() APIClient {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.HTTPClient.Timeout = 60 * time.Second
	retryableClient := rc.StandardClient()
	token := ""

	if kc := NewKeycloakTokenFetcherFromEnv(); kc != nil {
		if fetched, err := kc.Fetch(context.Background()); err == nil && fetched != "" {
			token = "Bearer " + fetched

			log.Infof("Fetched bearer token via KeycloakTokenFetcher")
		} else if err != nil {
			log.Warnf("API_BEARER_TOKEN not set and Keycloak fetch failed: %v", err)
		}
	} else {
		log.Warn("API_BEARER_TOKEN not set; authenticated calls will fail")
	}

	return APIClient{
		baseURL:         viper.GetString("API_BASEURL"),
		retryableClient: retryableClient,
		token:           token,
		xAPIKey:         viper.GetString("API_X_API_KEY"),
	}
}

func (clt APIClient) Get(url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if clt.token != "" {
		req.Header.Add("Authorization", clt.token)
	}

	if clt.xAPIKey != "" {
		req.Header.Add("x-api-key", clt.xAPIKey)
	}

	return clt.retryableClient.Do(req)
}

func (clt APIClient) Post(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		url,
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, err
	}

	if clt.token != "" {
		req.Header.Add("Authorization", clt.token)
	}

	if clt.xAPIKey != "" {
		req.Header.Add("x-api-key", clt.xAPIKey)
	}

	req.Header.Add("Content-Type", "application/json")

	return clt.retryableClient.Do(req)
}

// GetGitOrganisations returns git organisations and their code hosting URLs.
func (clt APIClient) GetGitOrganisations() ([]common.Publisher, error) {
	page := 1
	perPage := 100
	publishers := make([]common.Publisher, 0, 25)

	for {
		reqURL := fmt.Sprintf("%s?page=%d&perPage=%d", joinPath(clt.baseURL, "/gitOrganisations"), page, perPage)

		res, err := clt.Get(reqURL)
		if err != nil {
			return nil, fmt.Errorf("can't get gitOrganisations %s: %w", reqURL, err)
		}

		log.Debugf("GET %s -> %s (rl-rem=%s)", reqURL, res.Status, res.Header.Get("RateLimit-Remaining"))

		if res.StatusCode < 200 || res.StatusCode > 299 {
			res.Body.Close()

			return nil, fmt.Errorf("can't get gitOrganisations %s: HTTP status %s", reqURL, res.Status)
		}

		var gitOrgs []GitOrganisation
		if err := json.NewDecoder(res.Body).Decode(&gitOrgs); err != nil {
			res.Body.Close()

			return nil, fmt.Errorf("can't parse GET %s response: %w", reqURL, err)
		}

		res.Body.Close()

		for _, org := range gitOrgs {
			var groups, repos []internalUrl.URL

			for _, ch := range org.CodeHosting {
				u, err := url.Parse(ch.URL)
				if err != nil {
					return nil, fmt.Errorf("can't parse codeHosting URL %s: %w", ch.URL, err)
				}

				isGroup := true
				if ch.Group != nil {
					isGroup = *ch.Group
				}

				if isGroup {
					groups = append(groups, (internalUrl.URL)(*u))
				} else {
					repos = append(repos, (internalUrl.URL)(*u))
				}
			}

			name := org.ID
			if org.Organisation != nil && org.Organisation.Label != "" {
				name = org.Organisation.Label
			}

			id := org.ID
			if org.Organisation != nil && org.Organisation.URI != "" {
				id = org.Organisation.URI
			}

			publishers = append(publishers, common.Publisher{
				ID:            id,
				Name:          name,
				Organizations: groups,
				Repositories:  repos,
			})
		}

		nextPage := parseNextPage(res.Header.Get("Link"))
		totalPages := headerInt(res.Header.Get("Total-Pages"))

		switch {
		case nextPage > page:
			page = nextPage

			continue
		case totalPages > 0 && page < totalPages:
			page++

			continue
		default:
			return publishers, nil
		}
	}
}

// PostRepository creates a new repository entry.
func (clt APIClient) PostRepository(
	repoURL string,
	name *string,
	description *string,
	publiccodeYml *string,
	active bool,
) (*Repository, error) {
	body, err := json.Marshal(repositoryRequest{
		RepositoryURL:    repoURL,
		Name:             name,
		Description:      description,
		PubliccodeYmlURL: publiccodeYml,
		Active:           active,
	})
	if err != nil {
		return nil, fmt.Errorf("can't marshal repository: %w", err)
	}

	endpoint := joinPath(clt.baseURL, "/repositories")
	log.Debugf(
		"POST %s (repoUrl=%s name=%s descPresent=%t publiccode=%t)",
		endpoint,
		repoURL,
		deref(name),
		description != nil,
		publiccodeYml != nil,
	)

	res, err := clt.Post(endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("can't create repository: %w", err)
	}

	defer res.Body.Close()

	log.Debugf("POST %s -> %s (rl-rem=%s)", endpoint, res.Status, res.Header.Get("RateLimit-Remaining"))

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("can't create repository: API replied with HTTP %s", res.Status)
	}

	created := &Repository{}
	if err := json.NewDecoder(res.Body).Decode(created); err != nil {
		return nil, fmt.Errorf("can't parse POST /repositories response: %w", err)
	}

	return created, nil
}

func joinPath(base string, paths ...string) string {
	u, err := url.Parse(base)
	if err != nil {
		log.Fatal(err)
	}

	for _, p := range paths {
		u.Path = path.Join(u.Path, p)
	}

	return u.String()
}

func parseNextPage(linkHeader string) int {
	if linkHeader == "" {
		return 0
	}

	parts := strings.Split(linkHeader, ",")
	for _, part := range parts {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}

		start, end := strings.Index(part, "<"), strings.Index(part, ">")
		if start == -1 || end == -1 || end <= start+1 {
			continue
		}

		link := strings.TrimSpace(part[start+1 : end])

		u, err := url.Parse(link)
		if err != nil {
			continue
		}

		if pageStr := u.Query().Get("page"); pageStr != "" {
			if page, err := strconv.Atoi(pageStr); err == nil {
				return page
			}
		}
	}

	return 0
}

func headerInt(val string) int {
	if val == "" {
		return 0
	}

	i, _ := strconv.Atoi(val)

	return i
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
