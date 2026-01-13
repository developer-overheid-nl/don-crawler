package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/italia/publiccode-crawler/v4/cmd"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {
	log.SetLevel(log.DebugLevel)

	// Load .env into process environment if present so Viper can pick it up.
	if err := godotenv.Load(); err != nil {
		log.Debugf(".env not loaded: %v", err)
	}

	// Read configurations.
	viper.SetConfigName("config")
	viper.AddConfigPath(".")

	// Enable VIPER to read Environment Variables
	viper.AutomaticEnv()

	viper.SetDefault("DATADIR", "/app/data")
	viper.SetDefault("ACTIVITY_DAYS", 60)

	if err := viper.ReadInConfig(); err != nil {
		var notFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &notFoundError) {
			panic(fmt.Errorf("error reading config file: %w", err))
		}
	}

	if logPath := viper.GetString("LOG_FILE"); logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatalf("unable to open log file %s: %v", logPath, err)
		}

		log.SetOutput(io.MultiWriter(os.Stdout, f))
		log.Infof("Logging to %s", logPath)
	}

	log.Infof("DATADIR=%s", viper.GetString("DATADIR"))

	cmd.Execute()
}
