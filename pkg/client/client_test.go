package client_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
)

// mockCommandFactory creates commands that fail immediately
type mockCommandFactory struct{}

func (m *mockCommandFactory) New(name string, arg ...string) rubrics.Commander {
	return &mockCommander{}
}

// mockCommander implements rubrics.Commander and fails on Start/Run
type mockCommander struct{}

func (m *mockCommander) SetDir(dir string)          {}
func (m *mockCommander) SetStdin(stdin io.Reader)   {}
func (m *mockCommander) SetStdout(stdout io.Writer) {}
func (m *mockCommander) SetStderr(stderr io.Writer) {}
func (m *mockCommander) Start() error               { return context.DeadlineExceeded }
func (m *mockCommander) Run() error                 { return context.DeadlineExceeded }
func (m *mockCommander) ProcessKill() error         { return nil }

func TestWorkDirValidate(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name          string
		setup         func(t *testing.T) (string, func())
		wantErr       bool
		errContains   string
		skipOnWindows bool
	}

	cases := []testCase{
		{
			name: "empty path",
			setup: func(t *testing.T) (string, func()) {
				return "", nil
			},
			wantErr:     true,
			errContains: "not specified",
		},
		{
			name: "nonexistent path",
			setup: func(t *testing.T) (string, func()) {
				missing := filepath.Join(t.TempDir(), "does-not-exist")
				return missing, nil
			},
			wantErr:     true,
			errContains: "no such file or directory",
		},
		{
			name: "not a directory",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				file, err := os.CreateTemp(dir, "file")
				if err != nil {
					t.Fatalf("CreateTemp: %v", err)
				}
				if err := file.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
				return file.Name(), func() { _ = os.Remove(file.Name()) }
			},
			wantErr:     true,
			errContains: "not a directory",
		},
		{
			name: "open failure",
			setup: func(t *testing.T) (string, func()) {
				base := t.TempDir()
				restricted := filepath.Join(base, "restricted")
				if err := os.Mkdir(restricted, 0o700); err != nil {
					t.Fatalf("Mkdir: %v", err)
				}
				if err := os.Chmod(restricted, 0o100); err != nil {
					t.Fatalf("Chmod: %v", err)
				}
				return restricted, func() { _ = os.Chmod(restricted, 0o700) }
			},
			wantErr:       true,
			errContains:   "open",
			skipOnWindows: true,
		},
		{
			name: "success",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				return dir, nil
			},
		},
		{
			name: "success with contents",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "file"), []byte("data"), 0o600); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
				return dir, nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("directory permission semantics differ on Windows")
			}

			path, cleanup := tc.setup(t)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}

			err := client.WorkDir(path).Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error containing %q, got %v", tc.errContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected success, got: %v", err)
			}
		})
	}
}

func TestExecuteProject1(t *testing.T) {
	t.Parallel()
	type args struct {
		ctx context.Context
		cfg client.Config
	}
	tests := []struct {
		name             string
		args             args
		setupDir         func(t *testing.T) string // Function to create test directory
		wantErr          bool
		wantUploadCalls  int
		wantQualityCalls int
		checkOutput      func(t *testing.T, output string)
	}{
		{
			name: "nonexistent_directory",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "",
					RunCmd:         "",
					QualityClient:  &mockQualityServiceClient{},
					RubricClient:   &mockRubricServiceClient{},
					Writer:         &bytes.Buffer{},
					Reader:         strings.NewReader("y\n"), // Answer yes to upload
					CommandFactory: &mockCommandFactory{},    // Prevent subprocess execution
				},
			},
			setupDir: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nonexistent")
			},
			wantErr:          false, // Directory validation now happens at CLI level
			wantUploadCalls:  1,     // Should still upload results
			wantQualityCalls: 0,     // Quality service won't be called for nonexistent directory
			checkOutput: func(t *testing.T, output string) {
				// Output should still be generated, but Git evaluation will fail
				if !strings.Contains(output, "Git") {
					t.Errorf("expected output to contain Git rubric item")
				}
			},
		},
		{
			name: "success_path_no_upload",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "", // Will be set by setupDir
					RunCmd:         "",
					QualityClient:  nil,
					RubricClient:   nil,
					Writer:         &bytes.Buffer{},
					CommandFactory: &mockCommandFactory{}, // Prevent subprocess execution
				},
			},
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  0,
			wantQualityCalls: 0,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "Git Repository") {
					t.Error("expected Git Repository evaluation in output")
				}
			},
		},
		{
			name: "success_with_upload",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "", // Will be set by setupDir
					RunCmd:         "",
					QualityClient:  nil,
					RubricClient:   &mockRubricServiceClient{},
					Writer:         &bytes.Buffer{},
					Reader:         strings.NewReader("y\n"), // Answer yes to upload
					CommandFactory: &mockCommandFactory{},    // Prevent subprocess execution
				},
			},
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  1,
			wantQualityCalls: 0,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "Git Repository") {
					t.Error("expected Git Repository evaluation in output")
				}
			},
		},
		{
			name: "upload_error",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "", // Will be set by setupDir
					RunCmd:         "",
					QualityClient:  nil,
					RubricClient:   &mockRubricServiceClient{uploadError: errors.New("upload failed")},
					Writer:         &bytes.Buffer{},
					Reader:         strings.NewReader("y\n"), // Answer yes to upload
					CommandFactory: &mockCommandFactory{},    // Prevent subprocess execution
				},
			},
			setupDir:         createTestGitRepo,
			wantErr:          false, // Should not fail execution even if upload fails
			wantUploadCalls:  1,
			wantQualityCalls: 0,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "Git Repository") {
					t.Error("expected Git Repository evaluation in output")
				}
			},
		},
		{
			name: "failing_writer",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "", // Will be set by setupDir
					RunCmd:         "",
					QualityClient:  nil,
					RubricClient:   nil,
					Writer:         &failingWriter{},
					CommandFactory: &mockCommandFactory{}, // Prevent subprocess execution
				},
			},
			setupDir:         createTestGitRepo,
			wantErr:          false, // Render doesn't propagate writer errors (tablewriter library limitation)
			wantUploadCalls:  0,
			wantQualityCalls: 0,
			checkOutput: func(t *testing.T, output string) {
				// No output check needed - test verifies no panic with failing writer
			},
		},
		{
			name: "with_quality_client_success",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL: "http://example.com",
					Dir:       "", // Will be set by setupDir
					RunCmd:    "",
					QualityClient: &mockQualityServiceClient{
						qualityScore: 95,
						feedback:     "Excellent code quality",
					},
					RubricClient:   &mockRubricServiceClient{},
					Writer:         &bytes.Buffer{},
					Reader:         strings.NewReader("y\n"), // Answer yes to upload
					CommandFactory: &mockCommandFactory{},    // Prevent subprocess execution
				},
			},
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  1,
			wantQualityCalls: 1,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "Git Repository") {
					t.Error("expected Git Repository evaluation in output")
				}
				if !strings.Contains(output, "Quality") {
					t.Error("expected Quality evaluation in output")
				}
			},
		},
		{
			name: "with_quality_client_error",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL: "http://example.com",
					Dir:       "", // Will be set by setupDir
					RunCmd:    "",
					QualityClient: &mockQualityServiceClient{
						qualityError: errors.New("quality service unavailable"),
					},
					RubricClient:   nil,
					Writer:         &bytes.Buffer{},
					CommandFactory: &mockCommandFactory{}, // Prevent subprocess execution
				},
			},
			setupDir:         createTestGitRepo,
			wantErr:          false, // Quality errors shouldn't fail the whole execution
			wantUploadCalls:  0,
			wantQualityCalls: 1,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "Git Repository") {
					t.Error("expected Git Repository evaluation in output")
				}
				// Quality evaluation should still appear but with error points
				if !strings.Contains(output, "Quality") {
					t.Error("expected Quality evaluation in output even with error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Update the directory in the config
			tt.args.cfg.Dir = client.WorkDir(tt.setupDir(t))

			// Set up timeout context
			ctx, cancel := context.WithTimeout(tt.args.ctx, 5*time.Second)
			defer cancel()
			tt.args.ctx = ctx

			// Reset upload calls if using mock client
			if mockClient, ok := tt.args.cfg.RubricClient.(*mockRubricServiceClient); ok {
				mockClient.uploadCalls = 0
			}

			// Reset quality calls if using mock client
			if mockClient, ok := tt.args.cfg.QualityClient.(*mockQualityServiceClient); ok {
				mockClient.qualityCalls = 0
			}

			err := client.ExecuteProject1(tt.args.ctx, &tt.args.cfg)

			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteProject1() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check upload calls if we have a mock client
			if mockClient, ok := tt.args.cfg.RubricClient.(*mockRubricServiceClient); ok {
				if mockClient.uploadCalls != tt.wantUploadCalls {
					t.Errorf("expected %d upload calls, got %d", tt.wantUploadCalls, mockClient.uploadCalls)
				}
			}

			if mockClient, ok := tt.args.cfg.QualityClient.(*mockQualityServiceClient); ok {
				if mockClient.qualityCalls != tt.wantQualityCalls {
					t.Errorf("expected %d quality calls, got %d", tt.wantQualityCalls, mockClient.qualityCalls)
				}
			}

			// Check output if we have a buffer and no error
			if buf, ok := tt.args.cfg.Writer.(*bytes.Buffer); ok && err == nil {
				if buf.Len() == 0 {
					t.Fatal("expected output, got none")
				}
				tt.checkOutput(t, buf.String())
			}
		})
	}
}

// createTestGitRepo creates a temporary directory with a proper Git repository using go-git
func createTestGitRepo(t *testing.T) string {
	dir := t.TempDir()

	// Create README.md file
	if err := os.WriteFile(dir+"/README.md", []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Initialize Git repository using go-git (safe, no exec.Command)
	fs := osfs.New(dir)
	st := filesystem.NewStorage(osfs.New(dir+"/.git"), cache.NewObjectLRUDefault())

	repo, err := git.Init(st, fs)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	// Add and commit the file
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Test User", Email: "test@example.com"},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	return dir
}

// failingWriter always returns an error on Write
type failingWriter struct{}

func (f *failingWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write failed")
}

// mockRubricServiceClient implements protoconnect.RubricServiceClient for testing
type mockRubricServiceClient struct {
	uploadError error
	uploadCalls int
}

var _ protoconnect.RubricServiceClient = (*mockRubricServiceClient)(nil)

func (m *mockRubricServiceClient) UploadRubricResult(ctx context.Context, req *connect.Request[pb.UploadRubricResultRequest]) (*connect.Response[pb.UploadRubricResultResponse], error) {
	m.uploadCalls++
	if m.uploadError != nil {
		return nil, m.uploadError
	}

	response := &pb.UploadRubricResultResponse{
		SubmissionId: req.Msg.Result.SubmissionId,
		Message:      "upload successful",
	}
	return connect.NewResponse(response), nil
}

// mockQualityServiceClient implements protoconnect.QualityServiceClient for testing
type mockQualityServiceClient struct {
	qualityScore int32
	feedback     string
	qualityError error
	qualityCalls int
}

var _ protoconnect.QualityServiceClient = (*mockQualityServiceClient)(nil)

func (m *mockQualityServiceClient) EvaluateCodeQuality(ctx context.Context, req *connect.Request[pb.EvaluateCodeQualityRequest]) (*connect.Response[pb.EvaluateCodeQualityResponse], error) {
	m.qualityCalls++
	if m.qualityError != nil {
		return nil, m.qualityError
	}

	response := &pb.EvaluateCodeQualityResponse{
		QualityScore: m.qualityScore,
		Feedback:     m.feedback,
	}
	return connect.NewResponse(response), nil
}

type recordingRoundTripper struct {
	requests []*http.Request
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.requests = append(r.requests, req)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

type failingRoundTripper struct{}

func (f *failingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("transport failed: %w", errors.New("base error"))
}

func TestAuthTransport(t *testing.T) {
	t.Parallel()

	type args struct {
		token         string
		baseTransport http.RoundTripper
	}
	tests := []struct {
		name                  string
		args                  args
		requestHeader         string
		wantErr               bool
		wantAuthHeaderPrefix  string
		baseTransportErrorMsg string
	}{
		{
			name: "adds_authorization_header",
			args: args{
				token:         "test-token",
				baseTransport: &recordingRoundTripper{},
			},
			wantErr:              false,
			wantAuthHeaderPrefix: "Bearer test-token",
		},
		{
			name: "empty_token",
			args: args{
				token:         "",
				baseTransport: &recordingRoundTripper{},
			},
			wantErr:              false,
			wantAuthHeaderPrefix: "Bearer ",
		},
		{
			name: "overwrites_existing_auth_header",
			args: args{
				token:         "new-token",
				baseTransport: &recordingRoundTripper{},
			},
			requestHeader:        "Bearer old-token",
			wantErr:              false,
			wantAuthHeaderPrefix: "Bearer new-token",
		},
		{
			name: "propagates_base_transport_error",
			args: args{
				token:         "token",
				baseTransport: &failingRoundTripper{},
			},
			wantErr:               true,
			baseTransportErrorMsg: "transport failed",
		},
		{
			name: "uses_default_transport_when_nil",
			args: args{
				token:         "token",
				baseTransport: nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rt := client.NewAuthTransport(tt.args.token, tt.args.baseTransport)
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/test", http.NoBody)
			if tt.requestHeader != "" {
				req.Header.Set("Authorization", tt.requestHeader)
			}

			resp, err := rt.RoundTrip(req)
			if resp != nil {
				resp.Body.Close()
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("RoundTrip() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.baseTransportErrorMsg != "" {
				if !strings.Contains(err.Error(), tt.baseTransportErrorMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.baseTransportErrorMsg, err)
				}
				return
			}

			// For recording transport, verify the auth header was set correctly
			if recorder, ok := tt.args.baseTransport.(*recordingRoundTripper); ok && len(recorder.requests) > 0 {
				sentReq := recorder.requests[0]
				if tt.wantAuthHeaderPrefix != "" && !strings.HasPrefix(sentReq.Header.Get("Authorization"), tt.wantAuthHeaderPrefix) {
					t.Errorf("expected auth header to start with %q, got %q",
						tt.wantAuthHeaderPrefix, sentReq.Header.Get("Authorization"))
				}
			}
		})
	}
}

func TestExecuteProject2(t *testing.T) {
	t.Parallel()

	type args struct {
		ctx context.Context
		cfg client.Config
	}
	tests := []struct {
		name            string
		args            args
		setupDir        func(t *testing.T) string
		wantErr         bool
		wantUploadCalls int
		checkOutput     func(t *testing.T, output string)
	}{
		{
			name: "basic_execution_with_timeouts",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "",
					RunCmd:         "cat",
					QualityClient:  nil,
					RubricClient:   nil,
					Writer:         &bytes.Buffer{},
					Reader:         strings.NewReader("n\n"),
					CommandFactory: &mockCommandFactory{},
				},
			},
			setupDir: createTestGitRepo,
			wantErr:  false,
			checkOutput: func(t *testing.T, output string) {
				expectedItems := []string{"Git Repository", "DeleteExists", "MSetMGet", "TTLBasic", "Range", "Transactions"}
				for _, item := range expectedItems {
					if !strings.Contains(output, item) {
						t.Errorf("expected output to contain %q", item)
					}
				}
			},
		},
		{
			name: "with_upload_success",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "",
					RunCmd:         "cat",
					QualityClient:  nil,
					RubricClient:   &mockRubricServiceClient{},
					Writer:         &bytes.Buffer{},
					Reader:         strings.NewReader("y\n"),
					CommandFactory: &mockCommandFactory{},
				},
			},
			setupDir:        createTestGitRepo,
			wantErr:         false,
			wantUploadCalls: 1,
			checkOutput: func(t *testing.T, output string) {
				expectedItems := []string{"Git Repository", "DeleteExists", "MSetMGet", "TTLBasic", "Range", "Transactions"}
				for _, item := range expectedItems {
					if !strings.Contains(output, item) {
						t.Errorf("expected output to contain %q", item)
					}
				}
			},
		},
		{
			name: "with_upload_error",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "",
					RunCmd:         "cat",
					QualityClient:  nil,
					RubricClient:   &mockRubricServiceClient{uploadError: errors.New("upload failed")},
					Writer:         &bytes.Buffer{},
					Reader:         strings.NewReader("y\n"),
					CommandFactory: &mockCommandFactory{},
				},
			},
			setupDir:        createTestGitRepo,
			wantErr:         false,
			wantUploadCalls: 1,
			checkOutput: func(t *testing.T, output string) {
				expectedItems := []string{"Git Repository", "DeleteExists", "MSetMGet", "TTLBasic", "Range", "Transactions"}
				for _, item := range expectedItems {
					if !strings.Contains(output, item) {
						t.Errorf("expected output to contain %q", item)
					}
				}
			},
		},
		{
			name: "nonexistent_directory",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: client.Config{
					ServerURL:      "http://example.com",
					Dir:            "",
					RunCmd:         "echo test",
					QualityClient:  nil,
					RubricClient:   &mockRubricServiceClient{},
					Writer:         &bytes.Buffer{},
					Reader:         strings.NewReader("y\n"),
					CommandFactory: &mockCommandFactory{},
				},
			},
			setupDir: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nonexistent")
			},
			wantErr:         false,
			wantUploadCalls: 1,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "Git Repository") {
					t.Errorf("expected output to contain Git Repository evaluation")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Update the directory in the config
			tt.args.cfg.Dir = client.WorkDir(tt.setupDir(t))

			// Set up timeout context
			ctx, cancel := context.WithTimeout(tt.args.ctx, 5*time.Second)
			defer cancel()
			tt.args.ctx = ctx

			// Reset upload calls if using mock client
			if mockClient, ok := tt.args.cfg.RubricClient.(*mockRubricServiceClient); ok {
				mockClient.uploadCalls = 0
			}

			err := client.ExecuteProject2(tt.args.ctx, &tt.args.cfg)

			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteProject2() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check upload calls if we have a mock client
			if mockClient, ok := tt.args.cfg.RubricClient.(*mockRubricServiceClient); ok {
				if mockClient.uploadCalls != tt.wantUploadCalls {
					t.Errorf("expected %d upload calls, got %d", tt.wantUploadCalls, mockClient.uploadCalls)
				}
			}

			// Check output if we have a buffer and no error
			if buf, ok := tt.args.cfg.Writer.(*bytes.Buffer); ok && err == nil {
				if buf.Len() == 0 {
					t.Fatal("expected output, got none")
				}
				tt.checkOutput(t, buf.String())
			}
		})
	}
}
