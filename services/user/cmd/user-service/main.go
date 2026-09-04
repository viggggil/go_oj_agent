package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := newJSONLogger(os.Stdout)
	errorLogger := newJSONLogger(os.Stderr)

	app, cleanup, err := initApp()
	if err != nil {
		errorLogger.Error("failed to initialize user-service", "error", err)
		os.Exit(1)
	}
	defer func() {
		cleanup()
	}()

	logger.Info("starting user-service")
	if err := app.Run(); err != nil {
		errorLogger.Error("user-service exited with error", "error", err)
		os.Exit(1)
	}
}

func newJSONLogger(output *os.File) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{}))
}
