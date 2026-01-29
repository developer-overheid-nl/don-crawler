package scanner

import (
	"errors"
	"net/url"
	"time"

	"github.com/developer-overheid-nl/don-crawler/common"
)

var ErrPubliccodeNotFound = errors.New("publiccode.yml not found")

type Scanner interface {
	ScanRepo(url url.URL, publisher common.Publisher, repositories chan common.Repository) error
	ScanGroupOfRepos(url url.URL, publisher common.Publisher, repositories chan common.Repository) error
	LastCommitTimeFromAPI(url url.URL) (time.Time, error)
}
