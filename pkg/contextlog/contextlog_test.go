package contextlog_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	"github.com/stretchr/testify/assert"
)

func TestWith(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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
	logger.Info("test message")
	logger.Warn("test warning")
	logger.Error("test error")
}
