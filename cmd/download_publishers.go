package cmd

import (
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/developer-overheid-nl/don-crawler/common"
	ymlurl "github.com/developer-overheid-nl/don-crawler/internal"
	applog "github.com/developer-overheid-nl/don-crawler/internal/logging"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	rootCmd.AddCommand(downloadPublishersCmd)
}

type repolistType struct {
	Registrati []struct {
		IPA string `yaml:"ipa"`
		URL string `yaml:"url"`
		PEC string `yaml:"pec"`
	} `yaml:"registrati"`
}

var downloadPublishersCmd = &cobra.Command{
	Use:   "download-publishers REPOLIST_URL DEST_FILE",
	Short: "Download the list of repos and orgs from the onboarding portal.",
	Long:  `Download the list of repos and orgs from the onboarding portal and convert it into a publishers.yml.`,
	Args:  cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		var publishers []common.Publisher

		if _, err := os.Stat(args[1]); err == nil {
			data, err := os.ReadFile(args[1])
			if err != nil {
				applog.Event("command", "read_publishers").WithFields(map[string]any{
					applog.FieldPath:  args[1],
					applog.FieldError: err,
				}).Fatal("Publishers file could not be read")
			}
			//nolint:musttag // false positive
			_ = yaml.Unmarshal(data, &publishers)
		}

		resp, err := http.Get(args[0])
		if err != nil {
			applog.Event("command", "download_publishers").WithFields(map[string]any{
				"source":          args[0],
				applog.FieldError: err,
			}).Fatal("Publishers list could not be downloaded")
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			applog.Event("command", "read_publishers_response").WithError(err).Fatal("Publishers response could not be read")
		}

		var repolist repolistType

		err = yaml.Unmarshal(bodyBytes, &repolist)
		if err != nil {
			applog.Event("command", "decode_publishers").WithError(err).Fatal("Publishers response could not be decoded")
		}

	REPOLIST:
		for _, i := range repolist.Registrati {
			for idx, publisher := range publishers {
				if publisher.ID == i.IPA {
					u, _ := url.Parse(i.URL)
					// If this Id is already known, replace the org URL
					publishers[idx].Organization = (ymlurl.URL)(*u)
					publishers[idx].OrganisationURL = i.URL

					continue REPOLIST
				}
			}

			u, _ := url.Parse(i.URL)
			// If this IPA code is not known, append a new publisher item
			publishers = append(publishers, common.Publisher{
				Name:            i.IPA,
				ID:              i.IPA,
				Organization:    (ymlurl.URL)(*u),
				OrganisationURL: i.URL,
			})
		}

		// Write to the destination file
		f, err := os.Create(args[1])
		if err != nil {
			applog.Event("command", "create_publishers_file").WithFields(map[string]any{
				applog.FieldPath:  args[1],
				applog.FieldError: err,
			}).Fatal("Publishers file could not be created")
		}
		defer f.Close()

		data, err := yaml.Marshal(publishers)
		if err != nil {
			applog.Event("command", "encode_publishers").WithError(err).Fatal("Publishers could not be encoded")
		}

		if _, err = f.Write(data); err != nil {
			applog.Event("command", "write_publishers").WithFields(map[string]any{
				applog.FieldPath:  args[1],
				applog.FieldError: err,
			}).Fatal("Publishers file could not be written")
		}
	},
}
