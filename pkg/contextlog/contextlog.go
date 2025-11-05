// Package contextlog provides utilities for managing loggers through context.Context.
package contextlog

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

// With returns a new context with the given logger attached.
func With(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// From retrieves the logger from the context.
// If no logger is found, it returns the default logger.
func From(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(loggerKey{}).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return logger
}

// DiscardLogger returns a logger that discards all output.
// Useful for tests to suppress logging.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
