package crawler

import (
	"net/url"
	"testing"

	"github.com/developer-overheid-nl/don-crawler/common"
)

func mustURL(t *testing.T, raw string) url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}

	return *u
}

func TestTitleFromRepositoryName(t *testing.T) {
	tests := []struct {
		name       string
		repository common.Repository
		want       string
	}{
		{
			name: "from repository name",
			repository: common.Repository{
				Name: "Municipality-of-Rotterdam/Digitale-Balie",
			},
			want: "Digitale-Balie",
		},
		{
			name: "from canonical git url",
			repository: common.Repository{
				CanonicalURL: mustURL(t, "https://github.com/Municipality-of-Rotterdam/Digitale-Balie.git"),
			},
			want: "Digitale-Balie",
		},
		{
			name: "from canonical non git url",
			repository: common.Repository{
				CanonicalURL: mustURL(t, "https://github.com/owner/repo"),
			},
			want: "repo",
		},
		{
			name: "empty",
			repository: common.Repository{
				CanonicalURL: mustURL(t, "https://github.com"),
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := titleFromRepositoryName(tc.repository)
			if got != tc.want {
				t.Fatalf("titleFromRepositoryName() = %q, want %q", got, tc.want)
			}
		})
	}
}
