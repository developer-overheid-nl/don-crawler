package git

import (
	"errors"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestWithAuthTokenGitLabWithoutTokenFallsBackToAnonymous(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")

	auth, err := withAuthToken("gitlab.com", "https://gitlab.com/group/repo.git")
	if !errors.Is(err, errAnonymousGitLabAuth) {
		t.Fatalf("withAuthToken error = %v, want %v", err, errAnonymousGitLabAuth)
	}

	if auth != nil {
		t.Fatalf("withAuthToken auth = %T, want nil for anonymous clone", auth)
	}
}

func TestWithAuthTokenGitLabWithTokenUsesBasicAuth(t *testing.T) {
	const token = "test-token"
	t.Setenv("GITLAB_TOKEN", token)

	auth, err := withAuthToken("gitlab.com", "https://gitlab.com/group/repo.git")
	if err != nil {
		t.Fatalf("withAuthToken returned error: %v", err)
	}

	basicAuth, ok := auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatalf("withAuthToken auth = %T, want *http.BasicAuth", auth)
	}

	if basicAuth.Username != "oauth2" {
		t.Fatalf("basic auth username = %q, want %q", basicAuth.Username, "oauth2")
	}

	if basicAuth.Password != token {
		t.Fatalf("basic auth password = %q, want %q", basicAuth.Password, token)
	}
}
