package cmd

import (
	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/developer-overheid-nl/don-crawler/crawler"
	githubapp "github.com/developer-overheid-nl/don-crawler/internal/githubapp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func init() {
	crawlSoftwareCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "perform a dry run with no changes made")

	rootCmd.AddCommand(crawlSoftwareCmd)
}

var crawlSoftwareCmd = &cobra.Command{
	Use:   "crawl-software [SOFTWARE_ID | SOFTWARE_URL] PUBLISHER_ID",
	Short: "Crawl a single software by its id.",
	Long: `Crawl a single software by its id.

Crawl a single software given its API id and its publisher.`,
	Example: "# Crawl just the specified software\n" +
		"publiccode-crawler crawl-software" +
		" https://api.developer.overheid.nl/oss-register/v1/repositories/af6056fc-b2b2-4d31-9961-c9bd94e32bd4 PCM",

	Args: cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		if !githubapp.HasEnv() {
			log.Fatal("Please set GIT_OAUTH_CLIENTID/GIT_OAUTH_INSTALLATION_ID/GIT_OAUTH_SECRET to use the GitHub API")
		}

		c := crawler.NewCrawler(dryRun)

		publisher := common.Publisher{
			ID: args[1],
		}

		if err := c.CrawlSoftwareByID(args[0], publisher); err != nil {
			log.Fatal(err)
		}
	},
}
