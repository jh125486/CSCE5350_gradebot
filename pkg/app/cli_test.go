package app_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jh125486/CSCE5350_gradebot/pkg/app"
	basecli "github.com/jh125486/gradebot/pkg/cli"
	baseclient "github.com/jh125486/gradebot/pkg/client"
	"github.com/jh125486/gradebot/pkg/contextlog"
	"github.com/jh125486/gradebot/pkg/rubrics"
)

// mockCommandFactory is a test mock for CommandBuilder that immediately fails
type mockCommandFactory struct{}

func (m *mockCommandFactory) New(name string, arg ...string) rubrics.Commander {
	return &mockCommander{}
}

// mockCommander is a test mock for Commander that fails on Start
type mockCommander struct{}

func (m *mockCommander) SetDir(dir string)          {} // no-op for test
func (m *mockCommander) SetEnv(env []string)        {} // no-op for test
func (m *mockCommander) SetStdin(stdin io.Reader)   {} // no-op for test
func (m *mockCommander) SetStdout(stdout io.Writer) {} // no-op for test
func (m *mockCommander) SetStderr(stderr io.Writer) {} // no-op for test
func (m *mockCommander) Start() error               { return context.DeadlineExceeded }
func (m *mockCommander) Run() error                 { return context.DeadlineExceeded }
func (m *mockCommander) ProcessKill() error         { return nil }

const (
	testServerURL     = "http://example.invalid"
	testRunCmd        = "echo test"
	testStdinNegative = "n\n"
)

func TestWorkDirValidate(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	testCases := []struct {
		name    string
		dir     baseclient.WorkDir
		wantErr bool
	}{
		{
			name:    "valid directory",
			dir:     baseclient.WorkDir(tempDir),
			wantErr: false,
		},
		{
			name:    "nonexistent directory",
			dir:     baseclient.WorkDir("./no-such-dir"),
			wantErr: true,
		},
		{
			name:    "empty directory",
			dir:     baseclient.WorkDir(""),
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

// TestProject1CmdRun and TestProject2CmdRun don't call t.Parallel(): Run
// goes through rubrics.Program.Run, which os.Chdir()s into WorkDir and
// restores the prior cwd on return -- process-global state that races
// across concurrent tests (see pkg/client/projects_test.go's identical note).
func TestProject1CmdRun(t *testing.T) {
	type args struct {
		serverURL string
		dir       string
		runCmd    string
		client    *http.Client
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			// gradebot's ExecuteProject fails fast on the initial program.Run
			// (see client.ExecuteProject), so a mock whose Start always fails
			// now surfaces as a hard error instead of being swallowed into
			// per-evaluator rubric notes.
			name: "mocked command factory that fails to start propagates the error",
			args: args{
				serverURL: testServerURL,
				dir:       t.TempDir(),
				runCmd:    testRunCmd,
				client: &http.Client{
					Timeout: 100 * time.Millisecond,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := app.Project1Cmd{
				CommonArgs: basecli.CommonArgs{
					ServerURL: tt.args.serverURL,
					WorkDir:   baseclient.WorkDir(tt.args.dir),
					RunCmd:    tt.args.runCmd,
				},
			}
			svc := &basecli.Service{
				Client:         tt.args.client,
				Stdout:         new(bytes.Buffer),
				CommandBuilder: (&mockCommandFactory{}).New,
			}

			ctx, cancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 100*time.Millisecond)
			defer cancel()

			err := p.Run(basecli.Context{Context: ctx}, svc)

			if (err != nil) != tt.wantErr {
				t.Errorf("Project1Cmd.Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProject2CmdRun(t *testing.T) {
	type args struct {
		serverURL string
		dir       string
		runCmd    string
		client    *http.Client
		stdin     io.Reader
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			// See TestProject1CmdRun: gradebot's ExecuteProject now fails
			// fast on the initial program.Run, so the always-failing mock
			// Start propagates as a hard error regardless of stdin.
			name: "mocked command factory that fails to start propagates the error with stdin",
			args: args{
				serverURL: testServerURL,
				dir:       t.TempDir(),
				runCmd:    testRunCmd,
				client:    &http.Client{Timeout: 100 * time.Millisecond},
				stdin:     strings.NewReader(testStdinNegative),
			},
			wantErr: true,
		},
		{
			name: "mocked command factory that fails to start propagates the error with nil stdin",
			args: args{
				serverURL: testServerURL,
				dir:       t.TempDir(),
				runCmd:    testRunCmd,
				client:    &http.Client{Timeout: 100 * time.Millisecond},
				stdin:     nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := app.Project2Cmd{
				CommonArgs: basecli.CommonArgs{
					ServerURL: tt.args.serverURL,
					WorkDir:   baseclient.WorkDir(tt.args.dir),
					RunCmd:    tt.args.runCmd,
				},
			}
			svc := &basecli.Service{
				Client:         tt.args.client,
				Stdout:         new(bytes.Buffer),
				Stdin:          tt.args.stdin,
				CommandBuilder: (&mockCommandFactory{}).New,
			}

			ctx, cancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 100*time.Millisecond)
			defer cancel()

			err := p.Run(basecli.Context{Context: ctx}, svc)

			if (err != nil) != tt.wantErr {
				t.Errorf("Project2Cmd.Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
