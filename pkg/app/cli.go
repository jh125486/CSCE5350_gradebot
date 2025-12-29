package app

import (
	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	basecli "github.com/jh125486/gradebot/pkg/cli"
	baseclient "github.com/jh125486/gradebot/pkg/client"
	"github.com/jh125486/gradebot/pkg/proto/protoconnect"
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
func (cmd *Project1Cmd) Run(ctx basecli.Context) error {
	cfg := &baseclient.Config{
		ServerURL:      cmd.ServerURL,
		Dir:            cmd.Dir,
		RunCmd:         cmd.RunCmd,
		QualityClient:  protoconnect.NewQualityServiceClient(cmd.Client, cmd.ServerURL),
		RubricClient:   protoconnect.NewRubricServiceClient(cmd.Client, cmd.ServerURL),
		Reader:         cmd.Stdin,
		Writer:         cmd.Stdout,
		CommandFactory: cmd.CommandFactory,
	}

	return client.ExecuteProject1(ctx, cfg)
}

// Run executes the Project 2 grading client.
func (cmd *Project2Cmd) Run(ctx basecli.Context) error {
	cfg := &baseclient.Config{
		ServerURL:      cmd.ServerURL,
		Dir:            cmd.Dir,
		RunCmd:         cmd.RunCmd,
		QualityClient:  protoconnect.NewQualityServiceClient(cmd.Client, cmd.ServerURL),
		RubricClient:   protoconnect.NewRubricServiceClient(cmd.Client, cmd.ServerURL),
		Reader:         cmd.Stdin,
		Writer:         cmd.Stdout,
		CommandFactory: cmd.CommandFactory,
	}

	return client.ExecuteProject2(ctx, cfg)
}
