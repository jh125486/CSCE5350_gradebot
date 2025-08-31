package client_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/bufbuild/connect-go"
	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
)

func TestAuthRoundTripper_SetsHeader(t *testing.T) {
	rt := client.NewAuthTransport("tkn", http.DefaultTransport)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer tkn" {
			t.Fatalf("authorization header not set: %v", r.Header)
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := &http.Client{Transport: rt}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := client.Do(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteProject1_NewResultError(t *testing.T) {
	// Setup
	cfg := client.Config{
		ServerURL: "http://example.invalid",
		Dir:       "./nonexistent-dir-should-fail",
		RunCmd:    "",
		Client:    http.DefaultClient,
		RubricClient: protoconnect.NewRubricServiceClient(
			http.DefaultClient,
			"http://example.invalid",
		),
		Writer: &bytes.Buffer{},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	// Test
	if err := client.ExecuteProject1(ctx, cfg); err != nil {
		t.Fatalf("expected no error from ExecuteProject1, got: %v", err)
	}
	// Since the directory is invalid, the produced rubric should contain
	// failure notes (Git/SetGet/Quality rows). Ensure the writer was written.
	if cfg.Writer != nil {
		if b, ok := cfg.Writer.(*bytes.Buffer); ok {
			if b.Len() == 0 {
				t.Fatalf("expected rubric table output, got empty")
			}
		}
	}
}

type successQualityServer struct {
	protoconnect.UnimplementedQualityServiceHandler
}

func (s *successQualityServer) EvaluateCodeQuality(ctx context.Context, req *connect.Request[pb.EvaluateCodeQualityRequest]) (*connect.Response[pb.EvaluateCodeQualityResponse], error) {
	return connect.NewResponse(&pb.EvaluateCodeQualityResponse{QualityScore: 80, Feedback: "nice"}), nil
}

func TestExecuteProject1_SuccessPath(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, handler := protoconnect.NewQualityServiceHandler(&successQualityServer{})
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(l) }()
	defer func() { _ = srv.Shutdown(t.Context()); l.Close() }()

	dir := createTestGitRepo(t)

	buf := &bytes.Buffer{}
	cfg := client.Config{
		ServerURL: "http://" + l.Addr().String(),
		Dir:       dir,
		RunCmd:    "",
		Client:    http.DefaultClient,
		RubricClient: protoconnect.NewRubricServiceClient(
			http.DefaultClient,
			"http://"+l.Addr().String(),
		),
		Writer: buf,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := client.ExecuteProject1(ctx, cfg); err != nil {
		t.Fatalf("ExecuteProject1 returned error: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("expected non-empty table output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("Quality")) || !bytes.Contains(buf.Bytes(), []byte("nice")) {
		t.Fatalf("output missing expected quality row: %s", buf.String())
	}
}

// createTestGitRepo creates a temporary directory with a proper Git repository using go-git
func createTestGitRepo(t *testing.T) string {
	dir := t.TempDir()

	// Create README.md file
	if err := os.WriteFile(dir+"/README.md", []byte("hello world"), 0644); err != nil {
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
