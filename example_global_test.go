package zlog_test

import "github.com/semihalev/zlog/v2"

func Example_globalLogger() {
	// Global logger is ready to use immediately
	zlog.Info("Starting application")

	// Log with key-value pairs (v0.x compatible)
	zlog.Info("User logged in", "username", "john", "user_id", 123)

	// Change log level
	zlog.SetLevel(zlog.LevelWarn)

	// This won't be logged (below threshold)
	zlog.Debug("Debug message")

	// This will be logged
	zlog.Warn("Low memory", "available", "512MB")
}

func ExampleSetDefault() {
	// Create a custom logger
	logger := zlog.NewStructured()
	logger.SetWriter(zlog.StderrWriter())
	logger.SetLevel(zlog.LevelDebug)

	// Set it as the global default
	zlog.SetDefault(logger)

	// Now all global calls use your custom logger
	zlog.Debug("This goes to stderr")
}
