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

// buildConfig assembles a client.Config shared by both projects' Run methods
// from the parsed CLI args and the injected Service.
func buildConfig(args basecli.CommonArgs, svc *basecli.Service) *baseclient.Config {
	cfg := &baseclient.Config{
		ServerURL:     args.ServerURL,
		WorkDir:       args.WorkDir,
		RunCmd:        args.RunCmd,
		Env:           args.Env,
		QualityClient: protoconnect.NewQualityServiceClient(svc.Client, args.ServerURL),
		RubricClient:  protoconnect.NewRubricServiceClient(svc.Client, args.ServerURL),
		Reader:        svc.Stdin,
		Writer:        svc.Stdout,
	}
	if svc.CommandBuilder != nil {
		cfg.ProgramBuilder = func(workDir, runCmd string) (baserubrics.ProgramRunner, error) {
			return baserubrics.New(workDir, runCmd, baserubrics.WithCommandBuilder(svc.CommandBuilder)), nil
		}
	}

	return cfg
}

// Run executes the Project 1 grading client.
func (cmd *Project1Cmd) Run(ctx basecli.Context, svc *basecli.Service) error {
	return client.ExecuteProject1(ctx, buildConfig(cmd.CommonArgs, svc))
}

// Run executes the Project 2 grading client.
func (cmd *Project2Cmd) Run(ctx basecli.Context, svc *basecli.Service) error {
	return client.ExecuteProject2(ctx, buildConfig(cmd.CommonArgs, svc))
}
