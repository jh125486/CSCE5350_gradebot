package rubrics_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v5"
	memfs "github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"

	r "github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
)

// failingChrootFS is a billy.Filesystem that fails on Chroot calls
type failingChrootFS struct {
	billy.Filesystem
}

func (f *failingChrootFS) Chroot(path string) (billy.Filesystem, error) {
	if path == ".git" {
		return nil, errors.New("chroot failed")
	}
	return f.Filesystem.Chroot(path)
}

// setupGitRepoWithCommit creates a git repo with an initial commit
//

func setupGitRepoWithCommit(t *testing.T) billy.Filesystem {
	t.Helper()
	fs := memfs.New()
	dot, err := fs.Chroot(".git")
	if err != nil {
		t.Fatalf("chroot: %v", err)
	}
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	repo, err := git.Init(st, fs)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	f, err := fs.Create("README.md")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_, _ = f.Write([]byte("hello"))
	_ = f.Close()
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@e.com"}})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return fs
}

// setupEmptyGitRepo creates a git repo without commits
//

func setupEmptyGitRepo(t *testing.T) billy.Filesystem {
	t.Helper()
	fs := memfs.New()
	dot, err := fs.Chroot(".git")
	if err != nil {
		t.Fatalf("chroot: %v", err)
	}
	st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
	_, err = git.Init(st, fs)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	return fs
}

// setupInvalidGitDir creates a .git directory without proper git structure
//

func setupInvalidGitDir(t *testing.T) billy.Filesystem {
	t.Helper()
	fs := memfs.New()
	err := fs.MkdirAll(".git", 0o755)
	if err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	return fs
}

// verifyGitEvaluation checks the evaluation result matches expectations
func verifyGitEvaluation(t *testing.T, item r.RubricItem, wantPoints float64, wantNoteContains string) {
	t.Helper()
	if wantPoints == 5 {
		if item.Note == "" {
			t.Fatalf("expected non-empty note from git evaluator")
		}
	} else {
		if item.Awarded != wantPoints {
			t.Fatalf("expected %v points, got %v", wantPoints, item.Awarded)
		}
		if !strings.Contains(item.Note, wantNoteContains) {
			t.Fatalf("expected note to contain '%s', got %s", wantNoteContains, item.Note)
		}
	}
}

func TestEvaluateGit(t *testing.T) {
	tests := []struct {
		name             string
		setupFS          func() billy.Filesystem
		wantPoints       float64
		wantNoteContains string
	}{
		{
			name:             "ReportsHead",
			setupFS:          func() billy.Filesystem { return setupGitRepoWithCommit(t) },
			wantPoints:       5,
			wantNoteContains: "",
		},
		{
			name:             "OpenFails",
			setupFS:          func() billy.Filesystem { return &failingChrootFS{Filesystem: memfs.New()} },
			wantPoints:       0,
			wantNoteContains: "failed to access .git directory",
		},
		{
			name:             "GitRepoOpenFails",
			setupFS:          func() billy.Filesystem { return setupInvalidGitDir(t) },
			wantPoints:       0,
			wantNoteContains: "failed to open git repo",
		},
		{
			name:             "HeadFails",
			setupFS:          func() billy.Filesystem { return setupEmptyGitRepo(t) },
			wantPoints:       0,
			wantNoteContains: "failed to get HEAD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := tt.setupFS()
			eval := r.EvaluateGit(fs)
			item := eval(nil, nil, nil)
			verifyGitEvaluation(t, item, tt.wantPoints, tt.wantNoteContains)
		})
	}
}
