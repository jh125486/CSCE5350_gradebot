package client

import (
	"context"
	_ "embed"

	"github.com/go-git/go-billy/v5/osfs"

	"github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
	"github.com/jh125486/gradebot/pkg/client"
	baserubrics "github.com/jh125486/gradebot/pkg/rubrics"
)

var (
	//go:embed instructions/project1.txt
	project1Instructions string

	//go:embed instructions/project2.txt
	project2Instructions string
)

// ExecuteProject1 executes the project1 grading flow using a runtime config.
func ExecuteProject1(ctx context.Context, cfg *client.Config) error {
	return client.ExecuteProject(ctx, cfg, "CSCE5350:Project1", project1Instructions,
		baserubrics.EvaluateGit(osfs.New(cfg.Dir.String())),
		rubrics.EvaluateDataFileCreated,
		rubrics.EvaluateSetGet,
		rubrics.EvaluateOverwriteKey,
		rubrics.EvaluateNonexistentGet,
		rubrics.EvaluatePersistenceAfterRestart,
	)
}

// ExecuteProject2 executes the project2 grading flow using a runtime config.
func ExecuteProject2(ctx context.Context, cfg *client.Config) error {
	return client.ExecuteProject(ctx, cfg, "CSCE5350:Project2", project2Instructions,
		baserubrics.EvaluateGit(osfs.New(cfg.Dir.String())),
		rubrics.EvaluateDeleteExists,
		rubrics.EvaluateMSetMGet,
		rubrics.EvaluateTTLBasic,
		rubrics.EvaluateRange,
		rubrics.EvaluateTransactions,
	)
}
