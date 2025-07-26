package zlog_test

import "github.com/semihalev/zlog/v2"

func Example_terminal() {
	// Create logger with beautiful terminal output
	logger := zlog.New()
	logger.SetWriter(zlog.StdoutTerminal())

	// Basic logging
	logger.Debug("Starting application")
	logger.Info("Server started successfully")
	logger.Warn("Memory usage is high")
	logger.Error("Failed to connect to database")

	// Output shows colored terminal format
}

func Example_structured() {
	// Create structured logger with terminal output
	logger := zlog.NewStructured()
	logger.SetWriter(zlog.StdoutTerminal())

	// Log with structured fields
	logger.Info("User logged in",
		zlog.String("username", "john"),
		zlog.Int("user_id", 12345),
		zlog.Bool("admin", true))

	logger.Error("Request failed",
		zlog.String("method", "POST"),
		zlog.String("path", "/api/users"),
		zlog.Int("status", 500),
		zlog.Float64("duration", 1.234))

	// Output shows colored terminal format with fields
}
