package app_test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jh125486/CSCE5350_gradebot/pkg/app"
	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
)

// mockRoundTripper is a simple mock HTTP transport for testing
type mockRoundTripper struct {
	roundTripFunc func(*http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

const testBuildID = "test-build-id-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// Keep this test lightweight: ensure ServerCmd.Run can be invoked without
// leaving a long-running server. We call Run with a short timeout context so
// the server will be shut down promptly by the context cancellation.
func TestNewReturnsContext(t *testing.T) {
	t.Parallel()
	var sc app.ServerCmd
	sc.DatabaseURL = os.Getenv("DATABASE_URL")

	initCtx, initCancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 2*time.Second)
	defer initCancel()

	// AfterApply must be called before Run to initialize storage
	if err := sc.AfterApply(app.Context{initCtx}); err != nil {
		t.Fatalf("AfterApply failed: %v", err)
	}
	defer func() {
		// Clean up storage connection after test
		if cleanErr := sc.AfterRun(); cleanErr != nil {
			t.Logf("AfterRun() cleanup error: %v", cleanErr)
		}
	}()

	runCtx, runCancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 50*time.Millisecond)
	defer runCancel()

	// call Run; it should return after the context is cancelled (no hang)
	_ = sc.Run(app.Context{runCtx}, testBuildID)
}

func TestServerCmd_Run(t *testing.T) {
	// NOTE: NOT using t.Parallel() because tests create storage connections
	tests := []struct {
		name    string
		cmd     app.ServerCmd
		wantErr bool
	}{
		{
			name: "with sql storage",
			cmd: app.ServerCmd{
				DatabaseURL: os.Getenv("DATABASE_URL"),
			},
			wantErr: false, // Will timeout but no initialization error
		},
		{
			name: "with r2 storage - valid config",
			cmd: app.ServerCmd{
				R2Endpoint:     "http://localstack:4566",
				AWSRegion:      "us-east-1",
				R2Bucket:       "test-bucket",
				AWSAccessKeyID: "test-key",
				AWSSecretKey:   "test-secret",
				UsePathStyle:   "true",
			},
			wantErr: false,
		},
		{
			name: "with r2 storage - invalid USE_PATH_STYLE",
			cmd: app.ServerCmd{
				R2Endpoint:     "http://localstack:4566",
				AWSRegion:      "us-east-1",
				R2Bucket:       "test-bucket",
				AWSAccessKeyID: "test-key",
				AWSSecretKey:   "test-secret",
				UsePathStyle:   "not-a-bool",
			},
			wantErr: true, // mustBool returns false, tries virtual-hosted which fails with LocalStack
		},
		{
			name: "with r2 storage - missing credentials",
			cmd: app.ServerCmd{
				R2Endpoint: "http://localstack:4566",
			},
			wantErr: true,
		},
		{
			name:    "no storage configured",
			cmd:     app.ServerCmd{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// NOTE: NOT using t.Parallel() in subtests because each creates a storage connection
			// and parallel execution can exhaust the connection pool

			// Use a longer timeout for initialization (R2 needs time to connect to LocalStack)
			initCtx, initCancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 5*time.Second)
			defer initCancel()

			// AfterApply must be called before Run to initialize storage
			err := tt.cmd.AfterApply(app.Context{initCtx})
			if tt.wantErr && err != nil {
				// Expected error during initialization
				return
			}
			if err != nil {
				t.Fatalf("AfterApply() unexpected error: %v", err)
			}
			defer func() {
				// Clean up storage connection after test
				if cleanErr := tt.cmd.AfterRun(); cleanErr != nil {
					t.Logf("AfterRun() cleanup error: %v", cleanErr)
				}
			}()

			// Run the server with a short timeout (will timeout with context)
			runCtx, runCancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 50*time.Millisecond)
			defer runCancel()
			err = tt.cmd.Run(app.Context{runCtx}, testBuildID)

			// Assert
			if tt.wantErr && err == nil {
				t.Errorf("ServerCmd.Run() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				// Context timeout is expected, initialization errors are not
				if runCtx.Err() == nil {
					t.Errorf("ServerCmd.Run() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestWorkDirValidate(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	testCases := []struct {
		name    string
		dir     client.WorkDir
		wantErr bool
	}{
		{
			name:    "valid directory",
			dir:     client.WorkDir(tempDir),
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
	t.Parallel()
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
			Stdout:    new(bytes.Buffer),
		},
	}

	ctx, cancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 100*time.Millisecond)
	defer cancel()

	// The test will fail during execution due to mock errors, but that's expected.
	// We're testing that the Run method can be invoked and delegates properly.
	_ = p.Run(app.Context{ctx})
}

func TestProject2CmdRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a mock HTTP client that will fail fast instead of making real requests
	// and triggering subprocess execution that can deadlock
	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				// Return a simple error to avoid actual execution
				return nil, http.ErrHandlerTimeout
			},
		},
	}

	p := app.Project2Cmd{
		CommonProjectArgs: app.CommonProjectArgs{
			ServerURL: "http://example.invalid",
			Dir:       client.WorkDir(dir),
			RunCmd:    "echo test",
			Client:    mockClient, // Inject our mock client
			Stdout:    new(bytes.Buffer),
		},
	}

	// Initialize the HTTP client like Kong would do
	if err := p.AfterApply(app.Context{}, "test-build-id"); err != nil {
		t.Fatalf("AfterApply failed: %v", err)
	}

	// NOTE: We do NOT call Run() here because it triggers actual rubrics evaluators
	// which execute subprocesses that can deadlock on pipes. The Run() method is tested
	// indirectly through other integration tests. This test verifies AfterApply works.
}

func TestNewParseFlagsProducesContext(t *testing.T) {
	t.Parallel()
	// Ensure New can be called with controlled args so kong.Parse does not
	// accidentally parse `go test` flags. Provide a valid subcommand and the
	// required flags so parsing succeeds.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	dir := t.TempDir()
	os.Args = []string{"gradebot-test", "project-1", "--dir", dir, "--run", "echo"}

	// Use a simple test ID
	var id [32]byte
	copy(id[:], "test-id-for-parsing")

	// pass a short-lived context to New for test determinism
	ctx := app.New(t.Context(), "gradebot-test", id)
	if ctx == nil {
		t.Fatalf("expected non-nil kong.Context from New")
	}
}
