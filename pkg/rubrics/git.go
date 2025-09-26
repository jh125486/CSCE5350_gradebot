package rubrics

import (
	"context"
	"fmt"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// EvaluateGit returns an Evaluator which reports the repository HEAD hash
// from the provided billy filesystem. The returned Evaluator conforms to the
// package's Evaluator type so it can be run as part of the grading flow.
func EvaluateGit(repoFS billy.Filesystem) Evaluator {
	return func(_ context.Context, _ ProgramRunner, _ RunBag) RubricItem {
		rubricItem := func(msg string, awarded float64) RubricItem {
			return RubricItem{
				Name:    "Git Repository",
				Note:    msg,
				Awarded: awarded,
				Points:  5,
			}
		}

		// Use the repository's .git directory as storage
		dot, err := repoFS.Chroot(".git")
		if err != nil {
			return rubricItem(fmt.Sprintf("failed to access .git directory: %v", err), 0)
		}

		// NewStorage in go-git v5 requires an object cache argument.
		// Use the default LRU object cache implementation.
		st := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

		r, err := git.Open(st, repoFS)
		if err != nil {
			return rubricItem(fmt.Sprintf("failed to open git repo: %v", err), 0)
		}
		ref, err := r.Head()
		if err != nil {
			return rubricItem(fmt.Sprintf("failed to get HEAD: %v", err), 0)
		}

		return rubricItem("hash:"+ref.Hash().String(), 5)
	}
}
