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
)

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
					ServerURL:     "http://example.com",
					Dir:           "",
					RunCmd:        "",
					QualityClient: &mockQualityServiceClient{},
					RubricClient:  &mockRubricServiceClient{},
					Writer:        &bytes.Buffer{},
					Reader:        strings.NewReader("y\n"), // Answer yes to upload
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
					ServerURL:     "http://example.com",
					Dir:           "", // Will be set by setupDir
					RunCmd:        "",
					QualityClient: nil,
					RubricClient:  nil,
					Writer:        &bytes.Buffer{},
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
					ServerURL:     "http://example.com",
					Dir:           "", // Will be set by setupDir
					RunCmd:        "",
					QualityClient: nil,
					RubricClient:  &mockRubricServiceClient{},
					Writer:        &bytes.Buffer{},
					Reader:        strings.NewReader("y\n"), // Answer yes to upload
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
					ServerURL:     "http://example.com",
					Dir:           "", // Will be set by setupDir
					RunCmd:        "",
					QualityClient: nil,
					RubricClient:  &mockRubricServiceClient{uploadError: errors.New("upload failed")},
					Writer:        &bytes.Buffer{},
					Reader:        strings.NewReader("y\n"), // Answer yes to upload
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
					ServerURL:     "http://example.com",
					Dir:           "", // Will be set by setupDir
					RunCmd:        "",
					QualityClient: nil,
					RubricClient:  nil,
					Writer:        &failingWriter{},
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
					RubricClient: &mockRubricServiceClient{},
					Writer:       &bytes.Buffer{},
					Reader:       strings.NewReader("y\n"), // Answer yes to upload
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
					RubricClient: nil,
					Writer:       &bytes.Buffer{},
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

type mockRoundTripper struct {
	responses   map[string]*http.Response
	requests    []*http.Request
	errorOnPath string // If set, return error for requests containing this path
	forceError  error  // If set, always return this error
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.requests = append(m.requests, req)

	// Return forced error if set
	if m.forceError != nil {
		return nil, m.forceError
	}

	// Return error for specific path if set
	if m.errorOnPath != "" && strings.Contains(req.URL.Path, m.errorOnPath) {
		return nil, errors.New("mock network error")
	}

	// Mock quality service response
	if strings.Contains(req.URL.Path, "quality") {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"quality_score": 85, "feedback": "Good code quality"}`)),
			Header:     make(http.Header),
		}, nil
	}

	// Mock rubric upload response - can return error status for testing
	if strings.Contains(req.URL.Path, "rubric") {
		header := make(http.Header)
		header.Set("Content-Type", "application/proto")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"submission_id": "test-123", "message": "uploaded successfully"}`)),
			Header:     header,
		}, nil
	}

	// Default response
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

// testRoundTripper is a simple RoundTripper for testing
type testRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (t *testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.roundTripFunc(req)
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

type failingRoundTripper struct{}

func (f *failingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("transport failed: %w", errors.New("base error"))
}

func TestAuthTransport(t *testing.T) {
	t.Parallel()
	type args struct {
		token              string
		baseTransport      http.RoundTripper
		existingAuthHeader string
		executeProject     bool
		expectUploadError  bool
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		wantAuthHeader    string
		wantErrorContains string
		wantUploadCalls   int
	}{
		{
			name: "with_mock_base_transport",
			args: args{
				token: "test-token",
				baseTransport: &mockRoundTripper{
					responses: make(map[string]*http.Response),
					requests:  []*http.Request{},
				},
			},
			wantErr:        false,
			wantAuthHeader: "Bearer test-token",
		},
		{
			name: "base_transport_error",
			args: args{
				token:         "token",
				baseTransport: &failingRoundTripper{},
			},
			wantErr:           true,
			wantErrorContains: "transport failed",
		},
		{
			name: "empty_token",
			args: args{
				token: "",
				baseTransport: &mockRoundTripper{
					responses: make(map[string]*http.Response),
					requests:  []*http.Request{},
				},
			},
			wantErr:        false,
			wantAuthHeader: "Bearer ",
		},
		{
			name: "empty_token_with_upload_error",
			args: args{
				token: "",
				baseTransport: &mockRoundTripper{
					responses: make(map[string]*http.Response),
					requests:  []*http.Request{},
				},
				executeProject:    true,
				expectUploadError: true,
			},
			wantErr:         false,
			wantUploadCalls: 1,
		},
		{
			name: "header_overwrite",
			args: args{
				token: "new-token",
				baseTransport: &mockRoundTripper{
					responses: make(map[string]*http.Response),
					requests:  []*http.Request{},
				},
				existingAuthHeader: "Bearer old-token",
			},
			wantErr:        false,
			wantAuthHeader: "Bearer new-token",
		},
		{
			name: "header_overwrite_with_project_execution",
			args: args{
				token: "new-token",
				baseTransport: &mockRoundTripper{
					responses: make(map[string]*http.Response),
					requests:  []*http.Request{},
				},
				existingAuthHeader: "Bearer old-token",
				executeProject:     true,
			},
			wantErr:         false,
			wantAuthHeader:  "Bearer new-token",
			wantUploadCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rt := client.NewAuthTransport(tt.args.token, tt.args.baseTransport)
			httpClient := &http.Client{Transport: rt}
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())

			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", http.NoBody)

			if tt.args.existingAuthHeader != "" {
				req.Header.Set("Authorization", tt.args.existingAuthHeader)
			}

			if tt.args.executeProject {
				// Test with actual project execution
				dir := createTestGitRepo(t)
				mockRubricClient := &mockRubricServiceClient{}
				if tt.args.expectUploadError {
					mockRubricClient.uploadError = errors.New("mock network error")
				}

				cfg := client.Config{
					ServerURL:     "http://example.com",
					Dir:           client.WorkDir(dir),
					RunCmd:        "",
					QualityClient: nil,
					RubricClient:  mockRubricClient,
					Writer:        &bytes.Buffer{},
					Reader:        strings.NewReader("y\n"),
				}

				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()

				err := client.ExecuteProject1(ctx, &cfg)
				if (err != nil) != tt.wantErr {
					t.Errorf("ExecuteProject1() error = %v, wantErr %v", err, tt.wantErr)
				}

				if tt.wantUploadCalls > 0 && mockRubricClient.uploadCalls != tt.wantUploadCalls {
					t.Errorf("expected %d upload call(s), got %d", tt.wantUploadCalls, mockRubricClient.uploadCalls)
				}

				if tt.wantAuthHeader != "" {
					mockTransport, ok := tt.args.baseTransport.(*mockRoundTripper)
					if ok && len(mockTransport.requests) > 0 {
						for _, req := range mockTransport.requests {
							if req.Header.Get("Authorization") != tt.wantAuthHeader {
								t.Errorf("request had wrong auth header: got %s, want %s", req.Header.Get("Authorization"), tt.wantAuthHeader)
							}
						}
					}
				}
			} else {
				// Simple transport test
				resp, err := httpClient.Do(req)
				if resp != nil {
					defer resp.Body.Close()
				}

				if (err != nil) != tt.wantErr {
					t.Errorf("RoundTrip() error = %v, wantErr %v", err, tt.wantErr)
					return
				}

				if tt.wantErrorContains != "" && err != nil {
					if !strings.Contains(err.Error(), tt.wantErrorContains) {
						t.Errorf("expected error containing %q, got: %v", tt.wantErrorContains, err)
					}
				}

				if tt.wantAuthHeader != "" {
					// Check captured request
					switch rt := tt.args.baseTransport.(type) {
					case *testRoundTripper:
						// For testRoundTripper, check the original request
						if req.Header.Get("Authorization") != tt.wantAuthHeader {
							t.Errorf("expected auth header %q, got %q", tt.wantAuthHeader, req.Header.Get("Authorization"))
						}
					case *mockRoundTripper:
						// For mockRoundTripper, check captured requests
						if len(rt.requests) > 0 {
							sentReq := rt.requests[0]
							if sentReq.Header.Get("Authorization") != tt.wantAuthHeader {
								t.Errorf("expected auth header %q, got %q", tt.wantAuthHeader, sentReq.Header.Get("Authorization"))
							}
						}
					}
				}
			}
		})
	}
}

func TestExecuteProject2(t *testing.T) {
	t.Parallel()

	type args struct {
		serverURL     string
		dir           string // Empty means create test git repo, otherwise use this path
		runCmd        string
		qualityClient protoconnect.QualityServiceClient
		rubricClient  protoconnect.RubricServiceClient
		userInput     string
		timeout       time.Duration
	}
	tests := []struct {
		name               string
		args               args
		noParallel         bool // Set true for tests that can't run in parallel
		wantErr            bool
		wantOutputContains []string
		wantOutputNotEmpty bool
		wantUploadCalls    int
	}{
		{
			name: "basic_execution_with_timeouts",
			args: args{
				serverURL:     "http://example.com",
				dir:           "",    // Empty = create test git repo
				runCmd:        "cat", // Use cat which just echoes - won't crash
				qualityClient: nil,
				rubricClient:  nil,
				userInput:     "n\n", // Don't upload
				timeout:       2 * time.Second,
			},
			wantErr:            false,
			wantOutputNotEmpty: true,
			wantOutputContains: []string{"Git Repository", "DeleteExists", "MSetMGet", "TTLBasic", "Range", "Transactions"},
		},
		{
			name: "with_upload_success",
			args: args{
				serverURL:     "http://example.com",
				dir:           "", // Empty = create test git repo
				runCmd:        "cat",
				qualityClient: nil,
				rubricClient:  &mockRubricServiceClient{},
				userInput:     "y\n", // Upload
				timeout:       2 * time.Second,
			},
			wantErr:            false,
			wantOutputNotEmpty: true,
			wantOutputContains: []string{"Git Repository", "DeleteExists", "MSetMGet", "TTLBasic", "Range", "Transactions"},
			wantUploadCalls:    1,
		},
		{
			name: "with_upload_error",
			args: args{
				serverURL:    "http://example.com",
				dir:          "", // Empty = create test git repo
				runCmd:       "cat",
				rubricClient: &mockRubricServiceClient{uploadError: errors.New("upload failed")},
				userInput:    "y\n",
				timeout:      2 * time.Second,
			},
			wantErr:            false, // Upload errors don't fail execution
			wantOutputNotEmpty: true,
			wantOutputContains: []string{"Git Repository", "DeleteExists", "MSetMGet", "TTLBasic", "Range", "Transactions"},
			wantUploadCalls:    1,
		},
		{
			name: "nonexistent_directory",
			args: args{
				serverURL:     "http://example.com",
				dir:           "/nonexistent/path/that/does/not/exist",
				runCmd:        "echo test",
				rubricClient:  &mockRubricServiceClient{},
				qualityClient: nil,
				userInput:     "y\n",
				timeout:       2 * time.Second,
			},
			noParallel:         true,                       // Can't run in parallel - changes working directory
			wantErr:            false,                      // Directory validation happens at CLI level
			wantOutputNotEmpty: true,                       // Output still generated
			wantOutputContains: []string{"Git Repository"}, // At least Git evaluation will fail
			wantUploadCalls:    1,                          // Should still attempt upload
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.noParallel {
				t.Parallel()
			}

			output := &bytes.Buffer{}
			cfg := client.Config{
				ServerURL:     tt.args.serverURL,
				Dir:           client.WorkDir(tt.args.dir),
				RunCmd:        tt.args.runCmd,
				QualityClient: tt.args.qualityClient,
				RubricClient:  tt.args.rubricClient,
				Writer:        output,
				Reader:        strings.NewReader(tt.args.userInput),
			}

			ctx, cancel := context.WithTimeout(t.Context(), tt.args.timeout)
			defer cancel()
			ctx = contextlog.With(ctx, contextlog.DiscardLogger())

			err := client.ExecuteProject2(ctx, &cfg)

			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteProject2() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantOutputNotEmpty && output.Len() == 0 {
				t.Error("expected rubric output, got none")
			}

			for _, item := range tt.wantOutputContains {
				if !strings.Contains(output.String(), item) {
					t.Errorf("expected output to contain %q", item)
				}
			}

			if tt.wantUploadCalls > 0 {
				mockClient, ok := tt.args.rubricClient.(*mockRubricServiceClient)
				if !ok {
					t.Fatal("expected mockRubricServiceClient")
				}
				if mockClient.uploadCalls != tt.wantUploadCalls {
					t.Errorf("expected %d upload calls, got %d", tt.wantUploadCalls, mockClient.uploadCalls)
				}
			}
		})
	}
}
