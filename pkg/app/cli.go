package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/alecthomas/kong"

	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	"github.com/jh125486/CSCE5350_gradebot/pkg/openai"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/CSCE5350_gradebot/pkg/server"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

type (
	CLI struct {
		Server   ServerCmd   `cmd:"" hidden:"true" help:"Start the grading server"`
		Project1 Project1Cmd `cmd:"" help:"Execute project1 grading client"`
		Project2 Project2Cmd `cmd:"" help:"Execute project2 grading client"`
	}

	ServerCmd struct {
		Port      string `name:"port" default:"8080" help:"Port of the grading server"`
		OpenAIKey string `name:"openai-key" help:"OpenAI API key" env:"OPENAI_API_KEY"`
	}
	Project1Cmd struct {
		CommonProjectArgs
	}
	Project2Cmd struct {
		CommonProjectArgs
	}
	CommonProjectArgs struct {
		ServerURL string `name:"server-url" default:"https://gradebot-unt-fab5dc5c.koyeb.app" help:"URL of the grading server"`
		Dir       string `name:"dir" help:"Path to your project directory" required:""`
		RunCmd    string `name:"run" help:"Command to run your program" required:""`

		client *http.Client
	}
)

func (c *CommonProjectArgs) AfterApply(_ Context, buildID string) error {
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: client.NewAuthTransport(buildID, http.DefaultTransport),
	}
	c.client = httpClient
	return nil
}

// Wrap context to get around reflection issues in Bind()
type Context struct {
	context.Context
}

func New(ctx context.Context, name string, id [32]byte) *kong.Context {
	buildID := hex.EncodeToString(id[:])
	var cli CLI
	return kong.Parse(&cli,
		kong.Name(name),
		kong.UsageOnError(),
		kong.Bind(Context{ctx}, buildID),
	)
}

func (cmd *ServerCmd) Run(ctx Context, buildID string) error {
	// Initialize storage
	storageCfg := storage.NewConfig()
	r2Storage, err := storage.NewR2Storage(ctx, storageCfg)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	return server.Start(ctx, server.Config{
		ID:   buildID,
		Port: cmd.Port,
		OpenAIClient: openai.NewClient(cmd.OpenAIKey, &http.Client{
			Timeout: 35 * time.Second, // Slightly longer than the API timeout
		}),
		Storage: r2Storage,
	})
}

func (cmd *Project1Cmd) Run(ctx Context) error {
	return client.ExecuteProject1(ctx, client.Config{
		ServerURL: cmd.ServerURL,
		Dir:       cmd.Dir,
		RunCmd:    cmd.RunCmd,
		Client:    cmd.client,
		RubricClient: protoconnect.NewRubricServiceClient(
			cmd.client,
			cmd.ServerURL,
		),
	})
}

func (cmd *Project2Cmd) Run(ctx Context) error {
	return client.ExecuteProject2(ctx, client.Config{
		ServerURL: cmd.ServerURL,
		Dir:       cmd.Dir,
		RunCmd:    cmd.RunCmd,
		Client:    cmd.client,
		RubricClient: protoconnect.NewRubricServiceClient(
			cmd.client,
			cmd.ServerURL,
		),
	})
}
