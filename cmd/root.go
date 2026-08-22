package cmd

import (
	applog "github.com/developer-overheid-nl/don-crawler/internal/logging"
	"github.com/spf13/cobra"
)

var (
	dryRun  bool
	rootCmd = &cobra.Command{
		Use:   "publiccode-crawler",
		Short: "A crawler for publiccode.yml files.",
		Long: `A fast and robust publiccode.yml file crawler.
Complete documentation is available at https://github.com/italia/publiccode-crawler`,
		Run: func(cmd *cobra.Command, _ []string) {
			err := cmd.Help()
			if err != nil {
				applog.Event("command", "show_help").WithError(err).Fatal("Command help could not be shown")
			}
		},
	}
)

// Execute is the entrypoint for cmd package Cobra.
func Execute() error {
	return rootCmd.Execute()
}
