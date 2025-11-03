package client

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/bufbuild/connect-go"
	"github.com/go-git/go-billy/v5/osfs"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
)

//go:embed exclude.yaml
var configFS embed.FS

// WorkDir is a validated project directory path
type WorkDir string

// Validate implements Kong's Validatable interface for WorkDir validation
func (w WorkDir) Validate() error {
	path := string(w)
	if path == "" {
		return fmt.Errorf("work directory not specified")
	}

	info, err := os.Stat(path)
	if err != nil {
		return &DirectoryError{Err: err}
	}
	if !info.IsDir() {
		return fmt.Errorf("work directory %q is not a directory", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return &DirectoryError{Err: err}
	}
	defer f.Close()

	if _, err := f.Readdirnames(1); err != nil && err != io.EOF {
		return &DirectoryError{Err: err}
	}

	return nil
}

// String returns the string representation of WorkDir
func (w WorkDir) String() string {
	return string(w)
}

// DirectoryError represents an error related to directory access
type DirectoryError struct {
	Err error
}

func (e *DirectoryError) Error() string {
	return fmt.Sprintf("%v\n%s", e.Err, e.getPermissionHelp())
}

func (e *DirectoryError) Unwrap() error {
	return e.Err
}

func (e *DirectoryError) getPermissionHelp() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS help: System Preferences → Security & Privacy → Privacy → Full Disk Access\nOr try: chmod 755 /path/to/directory"
	case "windows":
		return "Windows help: Right-click folder → Properties → Security → Edit permissions\nOr run as Administrator"
	case "linux":
		return "Linux help: chmod 755 /path/to/directory\nOr check file ownership with: ls -la"
	default:
		return "Check directory permissions and ownership"
	}
}

// Config represents configuration for the grading client
type Config struct {
	ServerURL string

	// Execution specific fields
	Dir    WorkDir
	RunCmd string

	// Connect client for the QualityService
	QualityClient protoconnect.QualityServiceClient

	// Connect client for the RubricService
	RubricClient protoconnect.RubricServiceClient

	// Writer is where the resulting rubric table will be written. If nil, defaults to os.Stdout
	Writer io.Writer

	// Reader is where to read user input from. If nil, defaults to os.Stdin
	Reader io.Reader
}

// AuthTransport injects an Authorization header for every outgoing request.
type AuthTransport struct {
	base  http.RoundTripper
	token string
}

// NewAuthTransport creates a new AuthTransport with the given token.
// If base is nil, http.DefaultTransport is used.
func NewAuthTransport(token string, base http.RoundTripper) *AuthTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &AuthTransport{
		base:  base,
		token: token,
	}
}

// RoundTrip implements http.RoundTripper by adding an Authorization header to each request.
func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request to avoid mutating the original
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

// UploadResult prompts the user for confirmation and uploads the rubric result to the server.
// If RubricClient is nil, logs and returns without error.
// If Reader is nil, defaults to os.Stdin for user input.
func (cfg *Config) UploadResult(ctx context.Context, result *rubrics.Result) error {
	if cfg.RubricClient == nil {
		slog.Info("Skipping upload - no rubric client configured")
		return nil
	}

	if !promptForSubmission(cfg.Reader) {
		return nil
	}

	// Convert rubrics.Result to protobuf format
	rubricItems := make([]*proto.RubricItem, len(result.Rubric))
	for i, item := range result.Rubric {
		rubricItems[i] = &proto.RubricItem{
			Name:    item.Name,
			Points:  item.Points,
			Awarded: item.Awarded,
			Note:    item.Note,
		}
	}

	req := connect.NewRequest(&proto.UploadRubricResultRequest{
		Result: &proto.Result{
			SubmissionId: result.SubmissionID,
			Timestamp:    result.Timestamp.Format(time.RFC3339),
			Rubric:       rubricItems,
		},
	})

	resp, err := cfg.RubricClient.UploadRubricResult(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to upload result: %w", err)
	}

	slog.Info("Successfully uploaded rubric result",
		"submission_id", result.SubmissionID,
		"response", resp.Msg,
	)

	return nil
}

// promptForSubmission asks the user if they want to submit results to the server.
// Returns true if user confirms, false otherwise.
// Uses the provided reader for input, or os.Stdin if reader is nil.
// Accepts "y", "Y", "yes", "YES" (case-insensitive, whitespace-trimmed).
func promptForSubmission(reader io.Reader) bool {
	if reader == nil {
		reader = os.Stdin
	}

	fmt.Print("\nSubmit results to server? (y/n): ")
	bufReader := bufio.NewReader(reader)
	response, err := bufReader.ReadString('\n')
	if err != nil {
		slog.Warn("Failed to read user input", "error", err)
		return false
	}

	response = strings.TrimSpace(response)
	responseLower := strings.ToLower(response)
	return responseLower == "y" || responseLower == "yes"
}

// ExecuteProject1 executes the project1 grading flow using a runtime config.
func ExecuteProject1(ctx context.Context, cfg *Config) error {
	return executeProject(ctx, cfg, "project1",
		rubrics.EvaluateGit(osfs.New(cfg.Dir.String())),
		rubrics.EvaluateDataFileCreated,
		rubrics.EvaluateSetGet,
		rubrics.EvaluateOverwriteKey,
		rubrics.EvaluateNonexistentGet,
		rubrics.EvaluatePersistenceAfterRestart,
	)
}

// ExecuteProject2 executes the project2 grading flow using a runtime config.
func ExecuteProject2(ctx context.Context, cfg *Config) error {
	return executeProject(ctx, cfg, "project2",
		rubrics.EvaluateGit(osfs.New(cfg.Dir.String())),
		rubrics.EvaluateDeleteExists,
		rubrics.EvaluateMSetMGet,
		rubrics.EvaluateTTLBasic,
		rubrics.EvaluateRange,
		rubrics.EvaluateTransactions,
	)
}

func executeProject(ctx context.Context, cfg *Config, name string, items ...rubrics.Evaluator) error {
	factory := &rubrics.ExecCommandFactory{Context: ctx}
	program := rubrics.NewProgram(cfg.Dir.String(), cfg.RunCmd, factory)
	defer func() {
		if err := program.Kill(); err != nil {
			slog.Error("failed to kill program", slog.Any("error", err))
		}
	}()

	results := rubrics.NewResult()
	bag := make(rubrics.RunBag)

	// Reset to ensure clean state before running evaluators
	if err := rubrics.Reset(program); err != nil {
		slog.Error("failed to reset program state", slog.Any("error", err))
		return err
	}

	if cfg.QualityClient != nil {
		sourceFS := os.DirFS(program.Path())
		instructions := instructionsFor(name)
		items = append(items, rubrics.EvaluateQuality(cfg.QualityClient, sourceFS, configFS, instructions))
	}
	for _, item := range items {
		evalCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		results.Rubric = append(results.Rubric, item(evalCtx, program, bag))
		cancel()
	}

	// Print rubric table to configured writer (default to stdout)
	results.Render(cfg.Writer)

	// Upload the results to the server with user confirmation
	if err := cfg.UploadResult(ctx, results); err != nil {
		slog.Error("Failed to upload rubric result", "error", err)
		// Don't fail the whole execution just because upload failed
	}

	return nil
}
