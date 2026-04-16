package scanner

import (
	"testing"

	"github.com/google/go-github/v43/github"
	"github.com/ktrysmt/go-bitbucket"
	"github.com/stretchr/testify/assert"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestGitHubRepositoryIsFork(t *testing.T) {
	assert.True(t, githubRepositoryIsFork(&github.Repository{Fork: github.Bool(true)}))
	assert.False(t, githubRepositoryIsFork(&github.Repository{Fork: github.Bool(false)}))
	assert.False(t, githubRepositoryIsFork(nil))
}

func TestGitLabProjectIsFork(t *testing.T) {
	assert.True(t, gitlabProjectIsFork(&gitlab.Project{
		ForkedFromProject: &gitlab.ForkParent{ID: 1},
	}))
	assert.False(t, gitlabProjectIsFork(&gitlab.Project{}))
	assert.False(t, gitlabProjectIsFork(nil))
}

func TestBitbucketRepositoryIsFork(t *testing.T) {
	assert.True(t, bitbucketRepositoryIsFork(&bitbucket.Repository{
		Parent: &bitbucket.Repository{Slug: "upstream"},
	}))
	assert.False(t, bitbucketRepositoryIsFork(&bitbucket.Repository{}))
	assert.False(t, bitbucketRepositoryIsFork(nil))
}
