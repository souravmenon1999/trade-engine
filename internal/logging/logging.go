// internal/logging/logging.go
package logging

import (
	"log/slog" 
	"os"
)

var logger *slog.Logger

// InitLogger initializes the global logger based on the log level string.
func InitLogger(level string) {
	var logLevel slog.Level

	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo // Default to info if level is unknown
	}

	// Use a JSON handler for structured logging
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Set the default logger (optional, but makes using log functions easier)
	slog.SetDefault(logger)
}

// GetLogger returns the initialized logger.
func GetLogger() *slog.Logger {
	if logger == nil {
		// Fallback or panic if logger not initialized, depending on desired strictness
		// For this example, we'll just return a default logger and log a warning.
		slog.Default().Warn("Logger not initialized, using default.")
		return slog.Default()
	}
	return logger
}
