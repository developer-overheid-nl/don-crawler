package cmd

import (
	"github.com/developer-overheid-nl/don-crawler/apiclient"
	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/developer-overheid-nl/don-crawler/crawler"
	githubapp "github.com/developer-overheid-nl/don-crawler/internal/githubapp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func init() {
	crawlCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "perform a dry run with no changes made")

	rootCmd.AddCommand(crawlCmd)
}

var crawlCmd = &cobra.Command{
	Use:   "crawl [publishers.yml] [directory/*.yml ...]",
	Short: "Crawl publiccode.yml files in publishers' repos.",
	Long: `Crawl publiccode.yml files in publishers' repos.

When run with no arguments, the publishers are fetched from the API,
otherwise the passed YAML files are used.`,
	Example: `
# Crawl publishers fetched from the API
crawl

# Crawl using a specific publishers.yml file
crawl publishers.yml

# Crawl all YAML files in a specific directory
crawl directory/*.yml`,

	Args: cobra.MinimumNArgs(0),
	Run: func(_ *cobra.Command, args []string) {
		if !githubapp.HasEnv() {
			log.Fatal("Please set GIT_OAUTH_CLIENTID/GIT_OAUTH_INSTALLATION_ID/GIT_OAUTH_SECRET to use the GitHub API")
		}

		c := crawler.NewCrawler(dryRun)

		var publishers []common.Publisher

		if len(args) == 0 {
			var err error

			apiclient := apiclient.NewClient()

			publishers, err = apiclient.GetGitOrganisations()
			if err != nil {
				log.Fatal(err)
			}
		} else {
			for _, yamlFile := range args {
				filePublishers, err := common.LoadPublishers(yamlFile)
				if err != nil {
					log.Fatal(err)
				}

				publishers = append(publishers, filePublishers...)
			}
		}

		if err := c.CrawlPublishers(publishers); err != nil {
			log.Fatal(err)
		}
	},
}
