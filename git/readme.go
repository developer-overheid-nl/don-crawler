package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/italia/publiccode-crawler/v4/common"
	"github.com/spf13/viper"
)

// ErrReadmeNotFound is returned when the repository has no README at the root.
var ErrReadmeNotFound = errors.New("readme not found")

// ReadReadme returns the contents of a repository README from the bare clone.
func ReadReadme(repository common.Repository) (string, error) {
	if repository.Name == "" {
		return "", errors.New("cannot read readme without repository name")
	}

	vendor, repo := common.SplitFullName(repository.Name)
	path := filepath.Join(viper.GetString("DATADIR"), "repos", repository.URL.Host, vendor, repo, "gitClone")

	if _, err := os.Stat(path); err != nil {
		return "", err
	}

	out, err := exec.Command("git", "-C", path, "ls-tree", "--name-only", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cannot list repository files: %s: %w", out, err)
	}

	readmePath := pickReadmeName(strings.Split(string(out), "\n"))
	if readmePath == "" {
		return "", ErrReadmeNotFound
	}

	out, err = exec.Command("git", "-C", path, "show", "HEAD:"+readmePath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %s: %w", readmePath, out, err)
	}

	return string(out), nil
}

func pickReadmeName(names []string) string {
	preferred := []string{"README.md", "README.rst", "README.txt", "README"}
	byLower := make(map[string]string, len(names))

	for _, name := range names {
		trimmed := strings.TrimSpace(name)

		if trimmed == "" {
			continue
		}

		byLower[strings.ToLower(trimmed)] = trimmed
	}

	for _, candidate := range preferred {
		if name, ok := byLower[strings.ToLower(candidate)]; ok {
			return name
		}
	}

	for _, name := range names {
		trimmed := strings.TrimSpace(name)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(strings.ToLower(trimmed), "readme") {
			return trimmed
		}
	}

	return ""
}
