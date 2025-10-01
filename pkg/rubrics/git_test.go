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

//nolint:ireturn // This is normal for billy.Filesystem implementations
func (f *failingChrootFS) Chroot(path string) (billy.Filesystem, error) {
	if path == ".git" {
		return nil, errors.New("chroot failed")
	}
	return f.Filesystem.Chroot(path)
}

func TestEvaluateGit(t *testing.T) {
	tests := []struct {
		name             string
		setupFS          func() billy.Filesystem
		wantPoints       float64
		wantNoteContains string
	}{
		{
			name: "ReportsHead",
			setupFS: func() billy.Filesystem {
				fs := memfs.New()
				// create .git storage
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
			},
			wantPoints:       5,
			wantNoteContains: "",
		},
		{
			name: "OpenFails",
			setupFS: func() billy.Filesystem {
				return &failingChrootFS{Filesystem: memfs.New()}
			},
			wantPoints:       0,
			wantNoteContains: "failed to access .git directory",
		},
		{
			name: "GitRepoOpenFails",
			setupFS: func() billy.Filesystem {
				fs := memfs.New()
				// Create .git directory but with invalid/empty git structure
				err := fs.MkdirAll(".git", 0o755)
				if err != nil {
					t.Fatalf("mkdir .git: %v", err)
				}
				return fs
			},
			wantPoints:       0,
			wantNoteContains: "failed to open git repo",
		},
		{
			name: "HeadFails",
			setupFS: func() billy.Filesystem {
				fs := memfs.New()
				// create .git storage but no commits
				dot, err := fs.Chroot(".git")
				if err != nil {
					t.Fatalf("chroot: %v", err)
				}
				st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())
				_, err = git.Init(st, fs)
				if err != nil {
					t.Fatalf("init repo: %v", err)
				}
				// No commit, so HEAD should fail
				return fs
			},
			wantPoints:       0,
			wantNoteContains: "failed to get HEAD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := tt.setupFS()
			eval := r.EvaluateGit(fs)
			item := eval(nil, nil, nil)

			if tt.wantPoints == 5 {
				if item.Note == "" {
					t.Fatalf("expected non-empty note from git evaluator")
				}
			} else {
				if item.Awarded != tt.wantPoints {
					t.Fatalf("expected %v points, got %v", tt.wantPoints, item.Awarded)
				}
				if !strings.Contains(item.Note, tt.wantNoteContains) {
					t.Fatalf("expected note to contain '%s', got %s", tt.wantNoteContains, item.Note)
				}
			}
		})
	}
}
