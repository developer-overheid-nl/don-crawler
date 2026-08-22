package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/developer-overheid-nl/don-crawler/cmd"
	applog "github.com/developer-overheid-nl/don-crawler/internal/logging"
	commonlogging "github.com/developer-overheid-nl/don-register-common/logging"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

const applicationName = "don-crawler"

func main() {
	os.Exit(run())
}

func init() {
	logger, err := commonlogging.NewJSONLogger(os.Stdout, applicationName, "info")
	if err != nil {
		panic(fmt.Sprintf("configure bootstrap logging: %v", err))
	}

	slog.SetDefault(logger)
}

func run() int {
	// Load .env into process environment if present so Viper can pick it up.
	if err := godotenv.Load(); err != nil {
		applog.Event("application", "load_environment").WithError(err).Debug(".env not loaded")
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
			applog.Event("application", "read_config").WithError(err).Error("Config file could not be read")

			return 1
		}
	}

	logFile, err := configureLogging(os.Stdout)
	if err != nil {
		applog.Event("application", "configure_logging").WithError(err).Error("Logging could not be configured")

		return 1
	}

	defer func() {
		if err := logFile.Close(); err != nil {
			applog.Event("application", "close_log_output").WithError(err).Warn("Log output could not be closed")
		}
	}()

	if viper.GetBool("ENABLE_FILE_LOG") && strings.TrimSpace(viper.GetString("LOG_FILE")) != "" {
		applog.Event("application", "configure_logging").
			WithField("log_file", viper.GetString("LOG_FILE")).
			Info("File logging enabled")
	}

	applog.Event("application", "configure_data_directory").
		WithField("data_directory", viper.GetString("DATADIR")).
		Debug("Data directory configured")

	if err := cmd.Execute(); err != nil {
		return 1
	}

	return 0
}

func configureLogging(console io.Writer) (io.Closer, error) {
	logger, err := commonlogging.NewJSONLogger(
		console,
		applicationName,
		viper.GetString("LOG_LEVEL"),
	)
	if err != nil {
		return nil, fmt.Errorf("configure shared logger: %w", err)
	}

	logPath := strings.TrimSpace(viper.GetString("LOG_FILE"))
	if logPath == "" || !viper.GetBool("ENABLE_FILE_LOG") {
		slog.SetDefault(logger)

		return io.NopCloser(strings.NewReader("")), nil
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("unable to open log file %s: %w", logPath, err)
	}

	logger, err = commonlogging.NewJSONLogger(
		io.MultiWriter(console, f),
		applicationName,
		viper.GetString("LOG_LEVEL"),
	)
	if err != nil {
		_ = f.Close()

		return nil, fmt.Errorf("configure shared file logger: %w", err)
	}

	slog.SetDefault(logger)

	return f, nil
}
