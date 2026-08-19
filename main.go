package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/developer-overheid-nl/don-crawler/cmd"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {
	os.Exit(run())
}

func run() int {
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
	viper.SetDefault("ENABLE_FILE_LOG", false)
	viper.SetDefault("CLEANUP_GIT_CLONES", true)
	viper.SetDefault("LOG_LEVEL", "info")

	if err := viper.ReadInConfig(); err != nil {
		var notFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &notFoundError) {
			log.Errorf("error reading config file: %v", err)

			return 1
		}
	}

	logFile, err := configureLogging(os.Stderr)
	if err != nil {
		log.Error(err)

		return 1
	}

	defer func() {
		if err := logFile.Close(); err != nil {
			log.Warnf("unable to close log output: %v", err)
		}
	}()

	if viper.GetBool("ENABLE_FILE_LOG") && strings.TrimSpace(viper.GetString("LOG_FILE")) != "" {
		log.Infof("Logging to %s", viper.GetString("LOG_FILE"))
	}

	log.Debugf("DATADIR=%s", viper.GetString("DATADIR"))

	if err := cmd.Execute(); err != nil {
		return 1
	}

	return 0
}

func configureLogging(console io.Writer) (io.Closer, error) {
	levelName := strings.TrimSpace(viper.GetString("LOG_LEVEL"))
	if levelName == "" {
		levelName = "info"
	}

	level, err := log.ParseLevel(levelName)
	if err != nil {
		return nil, fmt.Errorf("invalid LOG_LEVEL %q: %w", levelName, err)
	}

	log.SetLevel(level)
	log.SetOutput(console)

	logPath := strings.TrimSpace(viper.GetString("LOG_FILE"))
	if logPath == "" || !viper.GetBool("ENABLE_FILE_LOG") {
		return io.NopCloser(strings.NewReader("")), nil
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("unable to open log file %s: %w", logPath, err)
	}

	log.SetOutput(io.MultiWriter(console, f))

	return f, nil
}
