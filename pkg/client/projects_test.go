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
	"github.com/jh125486/gradebot/pkg/proto/protoconnect"
	baserubrics "github.com/jh125486/gradebot/pkg/rubrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const echoTestCmd = "echo test"

// mockCommandFactory creates commands that don't actually run
type mockCommandFactory struct {
	failStart bool
}

func (m *mockCommandFactory) New(name string, arg ...string) baserubrics.Commander {
	return &mockCommander{failStart: m.failStart}
}

// programBuilderWith adapts a CommandBuilder into the Config.ProgramBuilder
// hook so tests can inject a fake process runner.
func programBuilderWith(cb baserubrics.CommandBuilder) func(workDir, runCmd string) (baserubrics.ProgramRunner, error) {
	return func(workDir, runCmd string) (baserubrics.ProgramRunner, error) {
		return baserubrics.New(workDir, runCmd, baserubrics.WithCommandBuilder(cb)), nil
	}
}

// newTestConfig builds a Config wired to the mock command factory, with
// io.Discard output and no upload prompt by default; opts override fields
// individual tests care about.
func newTestConfig(dir string, opts ...func(*gbclient.Config)) *gbclient.Config {
	cfg := &gbclient.Config{
		WorkDir:        gbclient.WorkDir(dir),
		RunCmd:         echoTestCmd,
		ProgramBuilder: programBuilderWith((&mockCommandFactory{}).New),
		Writer:         io.Discard,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

func withReader(r io.Reader) func(*gbclient.Config) {
	return func(cfg *gbclient.Config) { cfg.Reader = r }
}

func withRubricClient(c protoconnect.RubricServiceClient) func(*gbclient.Config) {
	return func(cfg *gbclient.Config) { cfg.RubricClient = c }
}

func withQualityClient(c protoconnect.QualityServiceClient) func(*gbclient.Config) {
	return func(cfg *gbclient.Config) { cfg.QualityClient = c }
}

// mockCommander implements Commander but doesn't actually execute anything
type mockCommander struct {
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
	failStart bool
}

func (m *mockCommander) SetDir(dir string)   {}
func (m *mockCommander) SetEnv(env []string) {}
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

// None of the tests below call t.Parallel(): client.ExecuteProject's
// underlying rubrics.Program.Run os.Chdir()s into WorkDir and restores the
// prior cwd on return, which is process-global state -- running these
// concurrently races on it (observed in CI as a spurious "failed to
// determine working directory: getwd: no such file or directory").
// execProject is ExecuteProject1 or ExecuteProject2, parameterizing the
// otherwise-identical test bodies below across both projects.
type execProject func(context.Context, *gbclient.Config) error

func TestExecuteProject(t *testing.T) {
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	projects := map[string]execProject{
		"Project1": client.ExecuteProject1,
		"Project2": client.ExecuteProject2,
	}

	for name, exec := range projects {
		t.Run(name+"/executes successfully with mock command factory", func(t *testing.T) {
			err := exec(ctx, newTestConfig(t.TempDir())) // nil Reader skips the upload prompt
			assert.NoError(t, err)
		})

		t.Run(name+"/evaluators run in the correct order", func(t *testing.T) {
			require.NoError(t, exec(ctx, newTestConfig(t.TempDir())))
		})

		t.Run(name+"/upload result configured", func(t *testing.T) {
			cfg := newTestConfig(t.TempDir(),
				withReader(strings.NewReader("y\n")),
				withRubricClient(&mockRubricServiceClient{}),
			)
			assert.NoError(t, exec(ctx, cfg))
		})

		t.Run(name+"/upload errors are logged but don't fail execution", func(t *testing.T) {
			cfg := newTestConfig(t.TempDir(),
				withReader(strings.NewReader("y\n")),
				withRubricClient(&mockRubricServiceClient{uploadErr: errors.New("upload failed")}),
			)
			assert.NoError(t, exec(ctx, cfg))
		})

		t.Run(name+"/QualityClient code path", func(t *testing.T) {
			cfg := newTestConfig(t.TempDir(),
				withReader(strings.NewReader("n\n")),
				withQualityClient(&mockQualityServiceClient{}),
			)
			assert.NoError(t, exec(ctx, cfg))
		})
	}
}
