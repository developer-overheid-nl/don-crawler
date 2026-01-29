package common //revive:disable:var-naming

import (
	"fmt"
	"os"

	url "github.com/developer-overheid-nl/don-crawler/internal"
	"gopkg.in/yaml.v2"
)

var fileReaderInject = os.ReadFile

type Publisher struct {
	ID              string    `yaml:"id" json:"id"`
	Name            string    `yaml:"name" json:"name"`
	Organization    url.URL   `yaml:"org" json:"organization"`
	Repositories    []url.URL `yaml:"repos" json:"repositories"`
	OrganisationURL string    `yaml:"organisationUrl,omitempty" json:"organisationUrl,omitempty"`
}

// LoadPublishers loads the publishers YAML file and returns a slice of Publisher.
func LoadPublishers(path string) ([]Publisher, error) {
	data, err := fileReaderInject(path)
	if err != nil {
		return nil, fmt.Errorf("error in reading `%s': %w", path, err)
	}

	var publishers []Publisher

	err = yaml.Unmarshal(data, &publishers)
	if err != nil {
		return nil, fmt.Errorf("error in parsing `%s': %w", path, err)
	}

	return publishers, nil
}
