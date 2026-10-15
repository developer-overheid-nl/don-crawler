package scanner

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/developer-overheid-nl/don-crawler/common"
)

func forgejoRepoJSON(serverURL, owner, name string, extra string) string {
	return fmt.Sprintf(`{
		"name": %q,
		"full_name": "%s/%s",
		"description": "desc %s",
		"html_url": "%s/%s/%s",
		"clone_url": "%s/%s/%s.git",
		"default_branch": "main",
		"empty": false,
		"fork": false,
		"mirror": false,
		"archived": false,
		"created_at": "2026-01-02T03:04:05Z",
		"updated_at": "2026-02-03T04:05:06Z"%s
	}`, name, owner, name, name, serverURL, owner, name, serverURL, owner, name, extra)
}

func collectRepositories(t *testing.T, scan func(chan common.Repository) error) []common.Repository {
	t.Helper()

	repositories := make(chan common.Repository, 100)

	if err := scan(repositories); err != nil {
		t.Fatalf("scan returned error: %v", err)
	}

	close(repositories)

	var result []common.Repository
	for repository := range repositories {
		result = append(result, repository)
	}

	return result
}

func mustParseURL(t *testing.T, raw string) url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) returned error: %v", raw, err)
	}

	return *u
}

func TestForgejoScanGroupOfReposPaginatesAndIncludesMirrors(t *testing.T) {
	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/orgs/Kiesraad/repos" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/v1/orgs/Kiesraad/repos?page=2>; rel="next"`, server.URL))
			_, _ = fmt.Fprintf(w, "[%s]", forgejoRepoJSON(server.URL, "Kiesraad", "abacus", ""))
		case "2":
			_, _ = fmt.Fprintf(w, "[%s]", forgejoRepoJSON(server.URL, "Kiesraad", "mirrored", `,
				"mirror": true, "fork": true, "archived": true,
				"original_url": "https://github.com/kiesraad/mirrored"`))
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	publisher := common.Publisher{ID: "kiesraad", Name: "Kiesraad"}
	orgURL := mustParseURL(t, server.URL+"/Kiesraad")

	repositories := collectRepositories(t, func(ch chan common.Repository) error {
		return NewForgejoScanner().ScanGroupOfRepos(orgURL, publisher, ch)
	})

	if len(repositories) != 2 {
		t.Fatalf("got %d repositories, want 2", len(repositories))
	}

	first := repositories[0]
	if first.Name != "Kiesraad/abacus" {
		t.Fatalf("Name = %q, want Kiesraad/abacus", first.Name)
	}

	if first.Title != "abacus" || first.Description != "desc abacus" {
		t.Fatalf("Title/Description = %q/%q", first.Title, first.Description)
	}

	if want := server.URL + "/Kiesraad/abacus/raw/branch/main/publiccode.yml"; first.FileRawURL != want {
		t.Fatalf("FileRawURL = %q, want %q", first.FileRawURL, want)
	}

	if want := server.URL + "/Kiesraad/abacus.git"; first.CanonicalURL.String() != want {
		t.Fatalf("CanonicalURL = %q, want %q", first.CanonicalURL.String(), want)
	}

	if first.URL.String() != first.CanonicalURL.String() {
		t.Fatalf("URL = %q, want canonical URL %q", first.URL.String(), first.CanonicalURL.String())
	}

	if first.GitBranch != "main" {
		t.Fatalf("GitBranch = %q, want main", first.GitBranch)
	}

	if !first.CreatedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("CreatedAt = %s", first.CreatedAt)
	}

	if !first.UpdatedAt.Equal(time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)) {
		t.Fatalf("UpdatedAt = %s", first.UpdatedAt)
	}

	if first.Publisher.ID != "kiesraad" {
		t.Fatalf("Publisher.ID = %q, want kiesraad", first.Publisher.ID)
	}

	mirror := repositories[1]
	if mirror.Name != "Kiesraad/mirrored" {
		t.Fatalf("Name = %q, want Kiesraad/mirrored", mirror.Name)
	}

	if !mirror.IsFork || !mirror.IsArchived {
		t.Fatalf("IsFork/IsArchived = %t/%t, want true/true", mirror.IsFork, mirror.IsArchived)
	}
}

func TestForgejoScanGroupOfReposFallsBackToUserRepos(t *testing.T) {
	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/users/someone/repos":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, "[%s]", forgejoRepoJSON(server.URL, "someone", "tool", ""))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repositories := collectRepositories(t, func(ch chan common.Repository) error {
		return NewForgejoScanner().ScanGroupOfRepos(mustParseURL(t, server.URL+"/someone/"), common.Publisher{}, ch)
	})

	if len(repositories) != 1 || repositories[0].Name != "someone/tool" {
		t.Fatalf("repositories = %+v, want someone/tool", repositories)
	}
}

func TestForgejoScanGroupOfReposSkipsEmptyRepositories(t *testing.T) {
	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "[%s,%s,%s]",
			forgejoRepoJSON(server.URL, "org", "empty", `, "empty": true`),
			forgejoRepoJSON(server.URL, "org", "nobranch", `, "default_branch": ""`),
			forgejoRepoJSON(server.URL, "org", "ok", ""),
		)
	}))
	defer server.Close()

	repositories := collectRepositories(t, func(ch chan common.Repository) error {
		return NewForgejoScanner().ScanGroupOfRepos(mustParseURL(t, server.URL+"/org"), common.Publisher{}, ch)
	})

	if len(repositories) != 1 || repositories[0].Name != "org/ok" {
		t.Fatalf("repositories = %+v, want only org/ok", repositories)
	}
}

func TestForgejoScanRepoKeepsOriginalURL(t *testing.T) {
	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/Logius/tool" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, forgejoRepoJSON(server.URL, "Logius", "tool", ""))
	}))
	defer server.Close()

	repoURL := mustParseURL(t, server.URL+"/Logius/tool.git")

	repositories := collectRepositories(t, func(ch chan common.Repository) error {
		return NewForgejoScanner().ScanRepo(repoURL, common.Publisher{}, ch)
	})

	if len(repositories) != 1 {
		t.Fatalf("got %d repositories, want 1", len(repositories))
	}

	if repositories[0].URL.String() != repoURL.String() {
		t.Fatalf("URL = %q, want %q", repositories[0].URL.String(), repoURL.String())
	}
}

func TestForgejoScanRepoReturnsErrorOnNotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	repositories := make(chan common.Repository, 1)

	err := NewForgejoScanner().ScanRepo(mustParseURL(t, server.URL+"/org/missing"), common.Publisher{}, repositories)
	if err == nil {
		t.Fatal("ScanRepo should return an error for a missing repository")
	}
}

func TestForgejoLastCommitTimeFromAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/Kiesraad/abacus/commits" {
			http.NotFound(w, r)

			return
		}

		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Fatalf("limit = %q, want 1", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"created":"2026-10-01T00:00:00Z","commit":{"committer":{"date":"2026-10-14T09:26:50Z"}}}]`)
	}))
	defer server.Close()

	got, err := NewForgejoScanner().LastCommitTimeFromAPI(mustParseURL(t, server.URL+"/Kiesraad/abacus.git"))
	if err != nil {
		t.Fatalf("LastCommitTimeFromAPI returned error: %v", err)
	}

	if want := time.Date(2026, 10, 14, 9, 26, 50, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("LastCommitTimeFromAPI = %s, want %s", got, want)
	}
}

func TestForgejoLastCommitTimeFromAPIWithoutCommits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	_, err := NewForgejoScanner().LastCommitTimeFromAPI(mustParseURL(t, server.URL+"/org/repo"))
	if err == nil {
		t.Fatal("LastCommitTimeFromAPI should return an error when there are no commits")
	}
}

func TestForgejoGetRetriesOn429(t *testing.T) {
	previousFallback := forgejoRateLimitFallbackWait
	forgejoRateLimitFallbackWait = time.Millisecond

	defer func() { forgejoRateLimitFallbackWait = previousFallback }()

	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	repositories := make(chan common.Repository, 1)

	err := NewForgejoScanner().ScanGroupOfRepos(mustParseURL(t, server.URL+"/org"), common.Publisher{}, repositories)
	if err != nil {
		t.Fatalf("ScanGroupOfRepos returned error: %v", err)
	}

	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestForgejoGetReturnsRateLimitErrorAfterMaxRetries(t *testing.T) {
	previousFallback := forgejoRateLimitFallbackWait
	forgejoRateLimitFallbackWait = time.Millisecond

	defer func() { forgejoRateLimitFallbackWait = previousFallback }()

	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++

		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	repositories := make(chan common.Repository, 1)

	err := NewForgejoScanner().ScanRepo(mustParseURL(t, server.URL+"/org/repo"), common.Publisher{}, repositories)

	var rateLimitErr RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("error = %v (%T), want RateLimitError", err, err)
	}

	if rateLimitErr.Provider != "Forgejo" {
		t.Fatalf("Provider = %q, want Forgejo", rateLimitErr.Provider)
	}

	if want := maxForgejoRateLimitRetries + 1; attempts != want {
		t.Fatalf("attempts = %d, want %d", attempts, want)
	}
}

func TestForgejoNextPageURL(t *testing.T) {
	header := http.Header{}
	header.Set("Link", `<https://code.overheid.nl/api/v1/orgs/X/repos?page=2>; rel="next",`+
		`<https://code.overheid.nl/api/v1/orgs/X/repos?page=5>; rel="last"`)

	if got := forgejoNextPageURL(header); got != "https://code.overheid.nl/api/v1/orgs/X/repos?page=2" {
		t.Fatalf("forgejoNextPageURL = %q", got)
	}

	if got := forgejoNextPageURL(http.Header{}); got != "" {
		t.Fatalf("forgejoNextPageURL without Link = %q, want empty", got)
	}
}
