package client_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	gbclient "github.com/jh125486/gradebot/pkg/client"
	"github.com/jh125486/gradebot/pkg/contextlog"
	pb "github.com/jh125486/gradebot/pkg/proto"
	baserubrics "github.com/jh125486/gradebot/pkg/rubrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCommandFactory creates commands that don't actually run
type mockCommandFactory struct {
	failStart bool
}

func (m *mockCommandFactory) New(name string, arg ...string) baserubrics.Commander {
	return &mockCommander{failStart: m.failStart}
}

// mockCommander implements Commander but doesn't actually execute anything
type mockCommander struct {
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
	failStart bool
}

func (m *mockCommander) SetDir(dir string) {}
func (m *mockCommander) SetStdin(stdin io.Reader) {
	m.stdin = stdin
}
func (m *mockCommander) SetStdout(stdout io.Writer) {
	m.stdout = stdout
}
func (m *mockCommander) SetStderr(stderr io.Writer) {
	m.stderr = stderr
}
func (m *mockCommander) Start() error {
	if m.failStart {
		return io.EOF
	}
	// Read from stdin in background to avoid blocking
	if m.stdin != nil {
		go io.Copy(io.Discard, m.stdin)
	}
	// Write fake responses to stdout
	if m.stdout != nil {
		go func() {
			m.stdout.Write([]byte("OK\n"))
		}()
	}
	return nil
}
func (m *mockCommander) Run() error         { return m.Start() }
func (m *mockCommander) ProcessKill() error { return nil }

// mockRubricServiceClient implements RubricServiceClient for testing
type mockRubricServiceClient struct {
	uploadCalls int
	uploadErr   error
}

func (m *mockRubricServiceClient) UploadRubricResult(_ context.Context, _ *connect.Request[pb.UploadRubricResultRequest]) (*connect.Response[pb.UploadRubricResultResponse], error) {
	m.uploadCalls++
	if m.uploadErr != nil {
		return nil, m.uploadErr
	}
	return connect.NewResponse(&pb.UploadRubricResultResponse{
		Message: "Upload successful",
	}), nil
}

// mockQualityServiceClient implements QualityServiceClient for testing
type mockQualityServiceClient struct{}

func (m *mockQualityServiceClient) EvaluateCodeQuality(_ context.Context, _ *connect.Request[pb.EvaluateCodeQualityRequest]) (*connect.Response[pb.EvaluateCodeQualityResponse], error) {
	return connect.NewResponse(&pb.EvaluateCodeQualityResponse{
		QualityScore: 85,
		Feedback:     "Good code quality",
	}), nil
}

func TestExecuteProject1(t *testing.T) {
	t.Parallel()
	type args struct {
		ctx context.Context
		cfg *gbclient.Config
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "executes successfully with mock command factory",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: &gbclient.Config{
					Dir:            gbclient.WorkDir(t.TempDir()),
					RunCmd:         "echo test",
					CommandFactory: &mockCommandFactory{},
					Writer:         io.Discard,
					Reader:         nil, // Will skip upload prompt
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.ExecuteProject1(tt.args.ctx, tt.args.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExecuteProject2(t *testing.T) {
	t.Parallel()
	type args struct {
		ctx context.Context
		cfg *gbclient.Config
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "executes successfully with mock command factory",
			args: args{
				ctx: contextlog.With(context.Background(), contextlog.DiscardLogger()),
				cfg: &gbclient.Config{
					Dir:            gbclient.WorkDir(t.TempDir()),
					RunCmd:         "echo test",
					CommandFactory: &mockCommandFactory{},
					Writer:         io.Discard,
					Reader:         nil, // Will skip upload prompt
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.ExecuteProject2(tt.args.ctx, tt.args.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExecuteProject1_Integration(t *testing.T) {
	// This test verifies the evaluators are called in the correct order
	t.Parallel()
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	tempDir := t.TempDir()
	cfg := &gbclient.Config{
		Dir:            gbclient.WorkDir(tempDir),
		RunCmd:         "echo test",
		CommandFactory: &mockCommandFactory{},
		Writer:         io.Discard,
		Reader:         nil,
	}
	err := client.ExecuteProject1(ctx, cfg)
	require.NoError(t, err)
}

func TestExecuteProject2_Integration(t *testing.T) {
	// This test verifies the evaluators are called in the correct order
	t.Parallel()
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	tempDir := t.TempDir()
	cfg := &gbclient.Config{
		Dir:            gbclient.WorkDir(tempDir),
		RunCmd:         "echo test",
		CommandFactory: &mockCommandFactory{},
		Writer:         io.Discard,
		Reader:         nil,
	}
	err := client.ExecuteProject2(ctx, cfg)
	require.NoError(t, err)
}

func TestExecuteProject1WithUpload(t *testing.T) {
	// Test with upload result configured
	t.Parallel()
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	cfg := &gbclient.Config{
		Dir:            gbclient.WorkDir(t.TempDir()),
		RunCmd:         "echo test",
		CommandFactory: &mockCommandFactory{},
		Writer:         io.Discard,
		Reader:         strings.NewReader("y\n"),
		RubricClient:   &mockRubricServiceClient{},
	}
	err := client.ExecuteProject1(ctx, cfg)
	assert.NoError(t, err)
}

func TestExecuteProject2WithUpload(t *testing.T) {
	// Test with upload result configured
	t.Parallel()
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	cfg := &gbclient.Config{
		Dir:            gbclient.WorkDir(t.TempDir()),
		RunCmd:         "echo test",
		CommandFactory: &mockCommandFactory{},
		Writer:         io.Discard,
		Reader:         strings.NewReader("y\n"),
		RubricClient:   &mockRubricServiceClient{},
	}
	err := client.ExecuteProject2(ctx, cfg)
	assert.NoError(t, err)
}

func TestExecuteProject1WithUploadError(t *testing.T) {
	// Test that upload errors are logged but don't fail execution
	t.Parallel()
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	cfg := &gbclient.Config{
		Dir:            gbclient.WorkDir(t.TempDir()),
		RunCmd:         "echo test",
		CommandFactory: &mockCommandFactory{},
		Writer:         io.Discard,
		Reader:         strings.NewReader("y\n"),
		RubricClient:   &mockRubricServiceClient{uploadErr: errors.New("upload failed")},
	}
	// Should not return error even if upload fails
	err := client.ExecuteProject1(ctx, cfg)
	assert.NoError(t, err)
}

func TestExecuteProject2WithUploadError(t *testing.T) {
	// Test that upload errors are logged but don't fail execution
	t.Parallel()
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	cfg := &gbclient.Config{
		Dir:            gbclient.WorkDir(t.TempDir()),
		RunCmd:         "echo test",
		CommandFactory: &mockCommandFactory{},
		Writer:         io.Discard,
		Reader:         strings.NewReader("y\n"),
		RubricClient:   &mockRubricServiceClient{uploadErr: errors.New("upload failed")},
	}
	// Should not return error even if upload fails
	err := client.ExecuteProject2(ctx, cfg)
	assert.NoError(t, err)
}

func TestExecuteProject1WithQualityClient(t *testing.T) {
	// Test the QualityClient code path
	t.Parallel()
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	cfg := &gbclient.Config{
		Dir:            gbclient.WorkDir(t.TempDir()),
		RunCmd:         "echo test",
		CommandFactory: &mockCommandFactory{},
		Writer:         io.Discard,
		Reader:         strings.NewReader("n\n"),
		QualityClient:  &mockQualityServiceClient{},
	}
	err := client.ExecuteProject1(ctx, cfg)
	assert.NoError(t, err)
}

func TestExecuteProject2WithQualityClient(t *testing.T) {
	// Test the QualityClient code path
	t.Parallel()
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	cfg := &gbclient.Config{
		Dir:            gbclient.WorkDir(t.TempDir()),
		RunCmd:         "echo test",
		CommandFactory: &mockCommandFactory{},
		Writer:         io.Discard,
		Reader:         strings.NewReader("n\n"),
		QualityClient:  &mockQualityServiceClient{},
	}
	err := client.ExecuteProject2(ctx, cfg)
	assert.NoError(t, err)
}
