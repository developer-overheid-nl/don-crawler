package cmd

import (
	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/developer-overheid-nl/don-crawler/crawler"
	githubapp "github.com/developer-overheid-nl/don-crawler/internal/githubapp"
	applog "github.com/developer-overheid-nl/don-crawler/internal/logging"
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
			applog.Event("command", "validate_github_auth").Fatal("GitHub App environment is not configured")
		}

		c := crawler.NewCrawler(dryRun)

		publisher := common.Publisher{
			ID: args[1],
		}

		if err := c.CrawlSoftwareByID(args[0], publisher); err != nil {
			applog.Event("command", "crawl_software").WithFields(map[string]any{
				"software":            args[0],
				applog.FieldPublisher: args[1],
				applog.FieldError:     err,
			}).Fatal("Software crawl failed")
		}
	},
}
