package app_test

import (
	"context"
	"encoding/hex"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jh125486/CSCE5350_gradebot/pkg/app"
	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
)

// mockRoundTripper is a simple mock HTTP transport for testing
type mockRoundTripper struct {
	roundTripFunc func(*http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

// Keep this test lightweight: ensure ServerCmd.Run can be invoked without
// leaving a long-running server. We call Run with a short timeout context so
// the server will be shut down promptly by the context cancellation.
func TestNewReturnsContext(t *testing.T) {
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

func TestWorkDirValidate(t *testing.T) {
	testCases := []struct {
		name    string
		dir     client.WorkDir
		wantErr bool
	}{
		{
			name:    "valid directory",
			dir:     client.WorkDir("."),
			wantErr: false,
		},
		{
			name:    "nonexistent directory",
			dir:     client.WorkDir("./no-such-dir"),
			wantErr: true,
		},
		{
			name:    "empty directory",
			dir:     client.WorkDir(""),
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.dir.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for dir %q, got nil", tc.dir)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for dir %q, got %v", tc.dir, err)
			}
		})
	}
}

func TestProject1CmdRun(t *testing.T) {
	dir := t.TempDir()

	// Create a mock HTTP client that will fail fast instead of making real requests
	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				// Return a simple error to avoid actual execution
				return nil, http.ErrHandlerTimeout
			},
		},
	}

	p := app.Project1Cmd{
		CommonProjectArgs: app.CommonProjectArgs{
			ServerURL: "http://example.invalid",
			Dir:       client.WorkDir(dir),
			RunCmd:    "echo test",
			Client:    mockClient, // Inject our mock client
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	// The test will fail during execution due to mock errors, but that's expected.
	// We're testing that the Run method can be invoked and delegates properly.
	_ = p.Run(app.Context{ctx})
}

func TestProject2CmdRun(t *testing.T) {
	// Since Project2 is not implemented yet, we just test that it doesn't panic
	p := app.Project2Cmd{
		CommonProjectArgs: app.CommonProjectArgs{
			ServerURL: "http://example.invalid",
			Dir:       client.WorkDir("."),
			RunCmd:    "echo test",
		},
	}

	// Initialize the HTTP client like Kong would do
	if err := p.AfterApply(app.Context{}, "test-build-id"); err != nil {
		t.Fatalf("AfterApply failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	// Project2 is not implemented yet, so we expect it to succeed (return nil)
	if err := p.Run(app.Context{ctx}); err != nil {
		t.Fatalf("unexpected error from Project2 run: %v", err)
	}
}

func TestNewParseFlagsProducesContext(t *testing.T) {
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
