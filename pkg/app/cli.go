package app

import (
	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	basecli "github.com/jh125486/gradebot/pkg/cli"
	baseclient "github.com/jh125486/gradebot/pkg/client"
	"github.com/jh125486/gradebot/pkg/proto/protoconnect"
	baserubrics "github.com/jh125486/gradebot/pkg/rubrics"
)

type (
	// CLI defines the command-line interface structure for the gradebot application.
	CLI struct {
		Project1 Project1Cmd `cmd:"" help:"Execute project1 grading client"`
		Project2 Project2Cmd `cmd:"" help:"Execute project2 grading client"`
	}
	// Project1Cmd defines the command structure for running Project 1 grading.
	Project1Cmd struct {
		basecli.CommonArgs
	}
	// Project2Cmd defines the command structure for running Project 2 grading.
	Project2Cmd struct {
		basecli.CommonArgs
	}
)

// Run executes the Project 1 grading client.
func (cmd *Project1Cmd) Run(ctx basecli.Context, svc *basecli.Service) error {
	cfg := &baseclient.Config{
		ServerURL:     cmd.ServerURL,
		WorkDir:       cmd.WorkDir,
		RunCmd:        cmd.RunCmd,
		Env:           cmd.Env,
		QualityClient: protoconnect.NewQualityServiceClient(svc.Client, cmd.ServerURL),
		RubricClient:  protoconnect.NewRubricServiceClient(svc.Client, cmd.ServerURL),
		Reader:        svc.Stdin,
		Writer:        svc.Stdout,
	}
	if svc.CommandBuilder != nil {
		cfg.ProgramBuilder = func(workDir, runCmd string) (baserubrics.ProgramRunner, error) {
			return baserubrics.New(workDir, runCmd, baserubrics.WithCommandBuilder(svc.CommandBuilder)), nil
		}
	}

	return client.ExecuteProject1(ctx, cfg)
}

// Run executes the Project 2 grading client.
func (cmd *Project2Cmd) Run(ctx basecli.Context, svc *basecli.Service) error {
	cfg := &baseclient.Config{
		ServerURL:     cmd.ServerURL,
		WorkDir:       cmd.WorkDir,
		RunCmd:        cmd.RunCmd,
		Env:           cmd.Env,
		QualityClient: protoconnect.NewQualityServiceClient(svc.Client, cmd.ServerURL),
		RubricClient:  protoconnect.NewRubricServiceClient(svc.Client, cmd.ServerURL),
		Reader:        svc.Stdin,
		Writer:        svc.Stdout,
	}
	if svc.CommandBuilder != nil {
		cfg.ProgramBuilder = func(workDir, runCmd string) (baserubrics.ProgramRunner, error) {
			return baserubrics.New(workDir, runCmd, baserubrics.WithCommandBuilder(svc.CommandBuilder)), nil
		}
	}

	return client.ExecuteProject2(ctx, cfg)
}
