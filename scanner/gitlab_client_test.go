package scanner

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestNewGitlabClientUsesAnonymousAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Private-Token"); got != "" {
			t.Fatalf("Private-Token header = %q, want empty", got)
		}

		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header = %q, want empty", got)
		}

		if got := r.Header.Get("Job-Token"); got != "" {
			t.Fatalf("Job-Token header = %q, want empty", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"project","path_with_namespace":"group/project","default_branch":"main","web_url":"https://gitlab.com/group/project","http_url_to_repo":"https://gitlab.com/group/project.git"}`))
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse returned error: %v", err)
	}

	client, err := newGitlabClient(*baseURL)
	if err != nil {
		t.Fatalf("newGitlabClient returned error: %v", err)
	}

	_, _, err = client.Projects.GetProject("group/project", &gitlab.GetProjectOptions{})
	if err != nil {
		t.Fatalf("GetProject returned error: %v", err)
	}
}
