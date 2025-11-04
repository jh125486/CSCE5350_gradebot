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

// Test constants
const (
	testServerURL                     = "http://example.com"
	testGitRepository                 = "Git Repository"
	testExpectedGitRepositoryInOutput = "expected Git Repository evaluation in output"
	testExpectedOutputToContain       = "expected output to contain %q"
)

// mockCommandFactory creates commands that fail immediately
type mockCommandFactory struct{}

func (m *mockCommandFactory) New(name string, arg ...string) rubrics.Commander {
	return &mockCommander{}
}

// mockCommander implements rubrics.Commander and fails on Start/Run
type mockCommander struct{}

func (m *mockCommander) SetDir(dir string)          {} // No-op for mock
func (m *mockCommander) SetStdin(stdin io.Reader)   {} // No-op for mock
func (m *mockCommander) SetStdout(stdout io.Writer) {} // No-op for mock
func (m *mockCommander) SetStderr(stderr io.Writer) {} // No-op for mock
func (m *mockCommander) Start() error               { return context.DeadlineExceeded }
func (m *mockCommander) Run() error                 { return context.DeadlineExceeded }
func (m *mockCommander) ProcessKill() error         { return nil }

func TestWorkDirValidate(t *testing.T) {
	t.Parallel()

	cases := createWorkDirTestCases(t)

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
			testWorkDirValidateResult(t, tc.wantErr, tc.errContains, err)
		})
	}
}

func createWorkDirTestCases(t *testing.T) []struct {
	name          string
	setup         func(t *testing.T) (string, func())
	wantErr       bool
	errContains   string
	skipOnWindows bool
} {
	t.Helper()
	return []struct {
		name          string
		setup         func(t *testing.T) (string, func())
		wantErr       bool
		errContains   string
		skipOnWindows bool
	}{
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
}

func testWorkDirValidateResult(t *testing.T, wantErr bool, errContains string, err error) {
	t.Helper()
	if wantErr {
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if errContains != "" && !strings.Contains(err.Error(), errContains) {
			t.Fatalf("expected error containing %q, got %v", errContains, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}
func resetMocks(cfg *client.Config) {
	if mockClient, ok := cfg.RubricClient.(*mockRubricServiceClient); ok {
		mockClient.uploadCalls = 0
	}
	if mockClient, ok := cfg.QualityClient.(*mockQualityServiceClient); ok {
		mockClient.qualityCalls = 0
	}
}

func resetMocksProject2(cfg *client.Config) {
	if mockClient, ok := cfg.RubricClient.(*mockRubricServiceClient); ok {
		mockClient.uploadCalls = 0
	}
}

func executeProject1TestCase(t *testing.T, ctx context.Context, cfg *client.Config, wantErr bool, wantUploadCalls, wantQualityCalls int, checkOutput func(t *testing.T, output string)) {
	t.Helper()
	err := client.ExecuteProject1(ctx, cfg)
	validateProject1Error(t, err, wantErr)
	if err != nil {
		return
	}
	validateProject1Mocks(t, cfg, wantUploadCalls, wantQualityCalls)
	validateProjectOutput(t, cfg, err, checkOutput)
}

func executeProject2TestCase(t *testing.T, ctx context.Context, cfg *client.Config, wantErr bool, wantUploadCalls int, checkOutput func(t *testing.T, output string)) {
	t.Helper()
	err := client.ExecuteProject2(ctx, cfg)
	validateProject2Error(t, err, wantErr)
	if err != nil {
		return
	}
	validateProject2Mocks(t, cfg, wantUploadCalls)
	validateProjectOutput(t, cfg, err, checkOutput)
}

func createProject1NonexistentDirConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "",
		QualityClient:  &mockQualityServiceClient{},
		RubricClient:   &mockRubricServiceClient{},
		Writer:         &bytes.Buffer{},
		Reader:         strings.NewReader("y\n"),
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject1SuccessNoUploadConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "",
		QualityClient:  nil,
		RubricClient:   nil,
		Writer:         &bytes.Buffer{},
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject1SuccessWithUploadConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "",
		QualityClient:  nil,
		RubricClient:   &mockRubricServiceClient{},
		Writer:         &bytes.Buffer{},
		Reader:         strings.NewReader("y\n"),
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject1UploadErrorConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "",
		QualityClient:  nil,
		RubricClient:   &mockRubricServiceClient{uploadError: errors.New("upload failed")},
		Writer:         &bytes.Buffer{},
		Reader:         strings.NewReader("y\n"),
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject1FailingWriterConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "",
		QualityClient:  nil,
		RubricClient:   nil,
		Writer:         &failingWriter{},
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject1QualitySuccessConfig() *client.Config {
	return &client.Config{
		ServerURL: testServerURL,
		Dir:       "",
		RunCmd:    "",
		QualityClient: &mockQualityServiceClient{
			qualityScore: 95,
			feedback:     "Excellent code quality",
		},
		RubricClient:   &mockRubricServiceClient{},
		Writer:         &bytes.Buffer{},
		Reader:         strings.NewReader("y\n"),
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject1QualityErrorConfig() *client.Config {
	return &client.Config{
		ServerURL: testServerURL,
		Dir:       "",
		RunCmd:    "",
		QualityClient: &mockQualityServiceClient{
			qualityError: errors.New("quality service unavailable"),
		},
		RubricClient:   nil,
		Writer:         &bytes.Buffer{},
		CommandFactory: &mockCommandFactory{},
	}
}

func checkProject1NonexistentDirOutput(t *testing.T, output string) {
	if !strings.Contains(output, "Git") {
		t.Errorf("expected output to contain Git rubric item")
	}
}

func checkProject1GitRepoOutput(t *testing.T, output string) {
	if !strings.Contains(output, testGitRepository) {
		t.Error(testExpectedGitRepositoryInOutput)
	}
}

func checkProject1QualitySuccessOutput(t *testing.T, output string) {
	if !strings.Contains(output, testGitRepository) {
		t.Error(testExpectedGitRepositoryInOutput)
	}
	if !strings.Contains(output, "Quality") {
		t.Error("expected Quality evaluation in output")
	}
}

func checkProject1QualityErrorOutput(t *testing.T, output string) {
	if !strings.Contains(output, testGitRepository) {
		t.Error(testExpectedGitRepositoryInOutput)
	}
	if !strings.Contains(output, "Quality") {
		t.Error("expected Quality evaluation in output even with error")
	}
}

func createProject1Tests() []struct {
	name string
	args struct {
		ctx context.Context
		cfg client.Config
	}
	setupDir         func(t *testing.T) string
	wantErr          bool
	wantUploadCalls  int
	wantQualityCalls int
	checkOutput      func(t *testing.T, output string)
} {
	return []struct {
		name string
		args struct {
			ctx context.Context
			cfg client.Config
		}
		setupDir         func(t *testing.T) string
		wantErr          bool
		wantUploadCalls  int
		wantQualityCalls int
		checkOutput      func(t *testing.T, output string)
	}{
		{
			name:             "nonexistent_directory",
			setupDir:         func(t *testing.T) string { return filepath.Join(t.TempDir(), "nonexistent") },
			wantErr:          false,
			wantUploadCalls:  1,
			wantQualityCalls: 0,
			checkOutput:      checkProject1NonexistentDirOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: context.Background(),
				cfg: *createProject1NonexistentDirConfig(),
			},
		},
		{
			name:             "success_path_no_upload",
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  0,
			wantQualityCalls: 0,
			checkOutput:      checkProject1GitRepoOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: context.Background(),
				cfg: *createProject1SuccessNoUploadConfig(),
			},
		},
		{
			name:             "success_with_upload",
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  1,
			wantQualityCalls: 0,
			checkOutput:      checkProject1GitRepoOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: context.Background(),
				cfg: *createProject1SuccessWithUploadConfig(),
			},
		},
		{
			name:             "upload_error",
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  1,
			wantQualityCalls: 0,
			checkOutput:      checkProject1GitRepoOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: context.Background(),
				cfg: *createProject1UploadErrorConfig(),
			},
		},
		{
			name:             "failing_writer",
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  0,
			wantQualityCalls: 0,
			checkOutput: func(t *testing.T, output string) {
				// No validation needed for failing writer test
			},
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: context.Background(),
				cfg: *createProject1FailingWriterConfig(),
			},
		},
		{
			name:             "with_quality_client_success",
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  1,
			wantQualityCalls: 1,
			checkOutput:      checkProject1QualitySuccessOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: context.Background(),
				cfg: *createProject1QualitySuccessConfig(),
			},
		},
		{
			name:             "with_quality_client_error",
			setupDir:         createTestGitRepo,
			wantErr:          false,
			wantUploadCalls:  0,
			wantQualityCalls: 1,
			checkOutput:      checkProject1QualityErrorOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: context.Background(),
				cfg: *createProject1QualityErrorConfig(),
			},
		},
	}
}

func TestExecuteProject1(t *testing.T) {
	t.Parallel()
	tests := createProject1Tests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.args.cfg.Dir = client.WorkDir(tt.setupDir(t))

			ctx, cancel := context.WithTimeout(tt.args.ctx, 5*time.Second)
			defer cancel()
			tt.args.ctx = ctx

			resetMocks(&tt.args.cfg)

			executeProject1TestCase(t, tt.args.ctx, &tt.args.cfg, tt.wantErr, tt.wantUploadCalls, tt.wantQualityCalls, tt.checkOutput)
		})
	}
}

func validateProject2Mocks(t *testing.T, cfg *client.Config, wantUploadCalls int) {
	t.Helper()
	if mockClient, ok := cfg.RubricClient.(*mockRubricServiceClient); ok {
		if mockClient.uploadCalls != wantUploadCalls {
			t.Errorf("expected %d upload calls, got %d", wantUploadCalls, mockClient.uploadCalls)
		}
	}
}

func validateProject2Error(t *testing.T, err error, wantErr bool) {
	t.Helper()
	if (err != nil) != wantErr {
		t.Errorf("ExecuteProject2() error = %v, wantErr %v", err, wantErr)
	}
}

func validateProject1Mocks(t *testing.T, cfg *client.Config, wantUploadCalls, wantQualityCalls int) {
	t.Helper()
	if mockClient, ok := cfg.RubricClient.(*mockRubricServiceClient); ok {
		if mockClient.uploadCalls != wantUploadCalls {
			t.Errorf("expected %d upload calls, got %d", wantUploadCalls, mockClient.uploadCalls)
		}
	}

	if mockClient, ok := cfg.QualityClient.(*mockQualityServiceClient); ok {
		if mockClient.qualityCalls != wantQualityCalls {
			t.Errorf("expected %d quality calls, got %d", wantQualityCalls, mockClient.qualityCalls)
		}
	}
}

func validateProject1Error(t *testing.T, err error, wantErr bool) {
	t.Helper()
	if (err != nil) != wantErr {
		t.Errorf("ExecuteProject1() error = %v, wantErr %v", err, wantErr)
	}
}

func validateProjectOutput(t *testing.T, cfg *client.Config, err error, checkOutput func(t *testing.T, output string)) {
	t.Helper()
	if buf, ok := cfg.Writer.(*bytes.Buffer); ok && err == nil {
		if buf.Len() == 0 {
			t.Fatal("expected output, got none")
		}
		checkOutput(t, buf.String())
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
			testAuthTransportCase(t, tt.args.token, tt.args.baseTransport, tt.requestHeader, tt.wantErr, tt.wantAuthHeaderPrefix, tt.baseTransportErrorMsg)
		})
	}
}

func testAuthTransportCase(t *testing.T, token string, baseTransport http.RoundTripper, requestHeader string, wantErr bool, wantAuthHeaderPrefix, baseTransportErrorMsg string) {
	t.Helper()
	rt := client.NewAuthTransport(token, baseTransport)
	ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, testServerURL+"/test", http.NoBody)
	if requestHeader != "" {
		req.Header.Set("Authorization", requestHeader)
	}

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}

	if (err != nil) != wantErr {
		t.Errorf("RoundTrip() error = %v, wantErr %v", err, wantErr)
		return
	}

	if wantErr && baseTransportErrorMsg != "" {
		if !strings.Contains(err.Error(), baseTransportErrorMsg) {
			t.Errorf("expected error containing %q, got: %v", baseTransportErrorMsg, err)
		}
		return
	}

	// For recording transport, verify the auth header was set correctly
	if recorder, ok := baseTransport.(*recordingRoundTripper); ok && len(recorder.requests) > 0 {
		sentReq := recorder.requests[0]
		if wantAuthHeaderPrefix != "" && !strings.HasPrefix(sentReq.Header.Get("Authorization"), wantAuthHeaderPrefix) {
			t.Errorf("expected auth header to start with %q, got %q",
				wantAuthHeaderPrefix, sentReq.Header.Get("Authorization"))
		}
	}
}

func createProject2BasicConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "cat",
		QualityClient:  nil,
		RubricClient:   nil,
		Writer:         &bytes.Buffer{},
		Reader:         strings.NewReader("n\n"),
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject2UploadSuccessConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "cat",
		QualityClient:  nil,
		RubricClient:   &mockRubricServiceClient{},
		Writer:         &bytes.Buffer{},
		Reader:         strings.NewReader("y\n"),
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject2UploadErrorConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "cat",
		QualityClient:  nil,
		RubricClient:   &mockRubricServiceClient{uploadError: errors.New("upload failed")},
		Writer:         &bytes.Buffer{},
		Reader:         strings.NewReader("y\n"),
		CommandFactory: &mockCommandFactory{},
	}
}

func createProject2NonexistentDirConfig() *client.Config {
	return &client.Config{
		ServerURL:      testServerURL,
		Dir:            "",
		RunCmd:         "echo test",
		QualityClient:  nil,
		RubricClient:   &mockRubricServiceClient{},
		Writer:         &bytes.Buffer{},
		Reader:         strings.NewReader("y\n"),
		CommandFactory: &mockCommandFactory{},
	}
}

func project2DefaultCheckOutput(t *testing.T, output string) {
	expectedItems := []string{testGitRepository, "DeleteExists", "MSetMGet", "TTLBasic", "Range", "Transactions"}
	for _, item := range expectedItems {
		if !strings.Contains(output, item) {
			t.Errorf(testExpectedOutputToContain, item)
		}
	}
}

func createProject2Tests() []struct {
	name string
	args struct {
		ctx context.Context
		cfg client.Config
	}
	setupDir        func(t *testing.T) string
	wantErr         bool
	wantUploadCalls int
	checkOutput     func(t *testing.T, output string)
} {
	return []struct {
		name string
		args struct {
			ctx context.Context
			cfg client.Config
		}
		setupDir        func(t *testing.T) string
		wantErr         bool
		wantUploadCalls int
		checkOutput     func(t *testing.T, output string)
	}{
		{
			name:            "basic_execution_with_timeouts",
			setupDir:        createTestGitRepo,
			wantErr:         false,
			wantUploadCalls: 0,
			checkOutput:     project2DefaultCheckOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: *createProject2BasicConfig(),
			},
		},
		{
			name:            "with_upload_success",
			setupDir:        createTestGitRepo,
			wantErr:         false,
			wantUploadCalls: 1,
			checkOutput:     project2DefaultCheckOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: *createProject2UploadSuccessConfig(),
			},
		},
		{
			name:            "with_upload_error",
			setupDir:        createTestGitRepo,
			wantErr:         false,
			wantUploadCalls: 1,
			checkOutput:     project2DefaultCheckOutput,
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: *createProject2UploadErrorConfig(),
			},
		},
		{
			name:            "nonexistent_directory",
			setupDir:        func(t *testing.T) string { return filepath.Join(t.TempDir(), "nonexistent") },
			wantErr:         false,
			wantUploadCalls: 1,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, testGitRepository) {
					t.Errorf("expected output to contain Git Repository evaluation")
				}
			},
			args: struct {
				ctx context.Context
				cfg client.Config
			}{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: *createProject2NonexistentDirConfig(),
			},
		},
	}
}

func TestExecuteProject2(t *testing.T) {
	t.Parallel()
	tests := createProject2Tests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.args.cfg.Dir = client.WorkDir(tt.setupDir(t))

			ctx, cancel := context.WithTimeout(tt.args.ctx, 5*time.Second)
			defer cancel()
			tt.args.ctx = ctx

			resetMocksProject2(&tt.args.cfg)

			executeProject2TestCase(t, tt.args.ctx, &tt.args.cfg, tt.wantErr, tt.wantUploadCalls, tt.checkOutput)
		})
	}
}
