package contextlog_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	"github.com/stretchr/testify/assert"
)

func TestWith(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	ctx := contextlog.With(t.Context(), logger)

	retrieved := contextlog.From(ctx)
	assert.Equal(t, logger, retrieved)
}

func TestFromNoLogger(t *testing.T) {
	t.Parallel()

	logger := contextlog.From(t.Context())
	assert.Equal(t, slog.Default(), logger)
}

func TestDiscardLogger(t *testing.T) {
	t.Parallel()

	logger := contextlog.DiscardLogger()
	assert.NotNil(t, logger)

	// Should not panic when logging
	logger.InfoContext(context.Background(), "test message")
	logger.WarnContext(context.Background(), "test warning")
	logger.ErrorContext(context.Background(), "test error")
}
