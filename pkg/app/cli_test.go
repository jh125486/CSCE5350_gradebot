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
				CommonArgs: basecli.CommonArgs{
					ServerURL:      tt.args.serverURL,
					Dir:            baseclient.WorkDir(tt.args.dir),
					RunCmd:         tt.args.runCmd,
					Client:         tt.args.client,
					Stdout:         new(bytes.Buffer),
					CommandFactory: tt.args.commandFactory,
				},
			}

			ctx, cancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 100*time.Millisecond)
			defer cancel()

			err := p.Run(basecli.Context{Context: ctx})

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
				CommonArgs: basecli.CommonArgs{
					ServerURL:      tt.args.serverURL,
					Dir:            baseclient.WorkDir(tt.args.dir),
					RunCmd:         tt.args.runCmd,
					Client:         tt.args.client,
					Stdout:         new(bytes.Buffer),
					Stdin:          tt.args.stdin,
					CommandFactory: tt.args.commandFactory,
				},
			}

			ctx, cancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 100*time.Millisecond)
			defer cancel()

			err := p.Run(basecli.Context{Context: ctx})

			if (err != nil) != tt.wantErr {
				t.Errorf("Project2Cmd.Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
