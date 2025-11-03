package app_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jh125486/CSCE5350_gradebot/pkg/app"
	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	"github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
)

// mockCommandFactory is a test mock for CommandFactory that immediately fails
type mockCommandFactory struct{}

func (m *mockCommandFactory) New(name string, arg ...string) rubrics.Commander {
	return &mockCommander{}
}

// mockCommander is a test mock for Commander that fails on Start
type mockCommander struct{}

func (m *mockCommander) SetDir(dir string)          {} // no-op for test
func (m *mockCommander) SetStdin(stdin io.Reader)   {} // no-op for test
func (m *mockCommander) SetStdout(stdout io.Writer) {} // no-op for test
func (m *mockCommander) SetStderr(stderr io.Writer) {} // no-op for test
func (m *mockCommander) Start() error               { return context.DeadlineExceeded }
func (m *mockCommander) Run() error                 { return context.DeadlineExceeded }
func (m *mockCommander) ProcessKill() error         { return nil }

const testBuildID = "test-build-id-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

const (
	testServerURL     = "http://example.invalid"
	testRunCmd        = "echo test"
	testStdinNegative = "n\n"
)

func TestServerCmd_Run(t *testing.T) {
	// NOTE: NOT using t.Parallel() because tests create storage connections
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("R2_ENDPOINT") == "" {
		t.Skip("Skipping ServerCmd.Run tests: no storage backend configured (set DATABASE_URL or R2_ENDPOINT)")
	}
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
			wantErr: false,
		},
		{
			name: "with bad sql storage",
			cmd: app.ServerCmd{
				DatabaseURL: "bad-database-url",
			},
			wantErr: true,
		},
		{
			name: "with r2 storage - valid config",
			cmd: app.ServerCmd{
				R2Endpoint:     os.Getenv("R2_ENDPOINT"),
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
	type args struct {
		serverURL      string
		dir            string
		runCmd         string
		client         *http.Client
		commandFactory rubrics.CommandFactory
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "executes project 1 with mocked command factory",
			args: args{
				serverURL: testServerURL,
				dir:       t.TempDir(),
				runCmd:    testRunCmd,
				client: &http.Client{
					Timeout: 100 * time.Millisecond,
				},
				commandFactory: &mockCommandFactory{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := app.Project1Cmd{
				CommonProjectArgs: app.CommonProjectArgs{
					ServerURL:      tt.args.serverURL,
					Dir:            client.WorkDir(tt.args.dir),
					RunCmd:         tt.args.runCmd,
					Client:         tt.args.client,
					Stdout:         new(bytes.Buffer),
					CommandFactory: tt.args.commandFactory,
				},
			}

			ctx, cancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 100*time.Millisecond)
			defer cancel()

			err := p.Run(app.Context{ctx})

			if (err != nil) != tt.wantErr {
				t.Errorf("Project1Cmd.Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProject2CmdRun(t *testing.T) {
	t.Parallel()
	type args struct {
		serverURL      string
		dir            string
		runCmd         string
		client         *http.Client
		stdin          io.Reader
		commandFactory rubrics.CommandFactory
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "executes project 2 with mocked command factory and stdin",
			args: args{
				serverURL:      testServerURL,
				dir:            t.TempDir(),
				runCmd:         testRunCmd,
				client:         &http.Client{Timeout: 100 * time.Millisecond},
				stdin:          strings.NewReader(testStdinNegative),
				commandFactory: &mockCommandFactory{},
			},
			wantErr: false,
		},
		{
			name: "executes project 2 with nil stdin",
			args: args{
				serverURL:      testServerURL,
				dir:            t.TempDir(),
				runCmd:         testRunCmd,
				client:         &http.Client{Timeout: 100 * time.Millisecond},
				stdin:          nil,
				commandFactory: &mockCommandFactory{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := app.Project2Cmd{
				CommonProjectArgs: app.CommonProjectArgs{
					ServerURL:      tt.args.serverURL,
					Dir:            client.WorkDir(tt.args.dir),
					RunCmd:         tt.args.runCmd,
					Client:         tt.args.client,
					Stdout:         new(bytes.Buffer),
					Stdin:          tt.args.stdin,
					CommandFactory: tt.args.commandFactory,
				},
			}

			ctx, cancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 100*time.Millisecond)
			defer cancel()

			err := p.Run(app.Context{ctx})

			if (err != nil) != tt.wantErr {
				t.Errorf("Project2Cmd.Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
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
