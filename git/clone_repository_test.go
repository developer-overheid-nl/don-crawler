package git

import (
	"testing"
)

func TestWithAuthTokenGitLabUsesAnonymousAuth(t *testing.T) {
	auth, err := withAuthToken("gitlab.com", "https://gitlab.com/group/repo.git")
	if err != nil {
		t.Fatalf("withAuthToken returned error: %v", err)
	}

	if auth != nil {
		t.Fatalf("withAuthToken auth = %T, want nil for anonymous clone", auth)
	}
}
