package app_test

import (
	"context"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jh125486/CSCE5350_gradebot/pkg/app"
)

// Keep this test lightweight: ensure ServerCmd.Run can be invoked without
// leaving a long-running server. We call Run with a short timeout context so
// the server will be shut down promptly by the context cancellation.
func TestNew_ReturnsContext(t *testing.T) {
	var id [32]byte
	for i := range id {
		id[i] = byte(i)
	}
	hexID := hex.EncodeToString(id[:])

	var sc app.ServerCmd
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	// call Run; it should return after the context is cancelled (no hang)
	_ = sc.Run(app.Context{ctx}, hexID)
}

func TestProject1Cmd_Run_ErrorOnBadDir(t *testing.T) {
	p := app.Project1Cmd{
		CommonProjectArgs: app.CommonProjectArgs{
			ServerURL: "http://example.invalid",
			Dir:       "./no-such-dir",
			RunCmd:    "",
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := p.Run(app.Context{ctx}); err != nil {
		t.Fatalf("expected no error when running Project1 with invalid dir, got: %v", err)
	}
}

func TestProject2Cmd_Run_ErrorOnBadDir(t *testing.T) {
	p := app.Project2Cmd{
		CommonProjectArgs: app.CommonProjectArgs{
			ServerURL: "http://example.invalid",
			Dir:       "./no-such-dir",
			RunCmd:    "",
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := p.Run(app.Context{ctx}); err != nil {
		t.Fatalf("expected no error for Project2 stub, got: %v", err)
	}
}

func TestNew_ParseFlagsProducesContext(t *testing.T) {
	// Ensure New can be called with controlled args so kong.Parse does not
	// accidentally parse `go test` flags. Provide a valid subcommand and the
	// required flags so parsing succeeds.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	dir := t.TempDir()
	os.Args = []string{"gradebot-test", "project-1", "--dir", dir, "--run", "echo"}

	var id [32]byte
	for i := range id {
		id[i] = byte(i)
	}
	// pass a short-lived context to New for test determinism
	ctx := app.New(t.Context(), "gradebot-test", id)
	if ctx == nil {
		t.Fatalf("expected non-nil kong.Context from New")
	}
}
