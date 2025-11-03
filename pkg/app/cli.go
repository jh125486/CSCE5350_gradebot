package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/alecthomas/kong"

	"github.com/jh125486/CSCE5350_gradebot/pkg/client"
	"github.com/jh125486/CSCE5350_gradebot/pkg/openai"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/CSCE5350_gradebot/pkg/server"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

type (
	// CLI defines the command-line interface structure for the gradebot application.
	CLI struct {
		Server   ServerCmd   `cmd:"" hidden:"true" help:"Start the grading server"`
		Project1 Project1Cmd `cmd:"" help:"Execute project1 grading client"`
		Project2 Project2Cmd `cmd:"" help:"Execute project2 grading client"`
	}
	// ServerCmd contains configuration for running the grading server.
	ServerCmd struct {
		Port      string `name:"port" default:"8080" help:"Port of the grading server"`
		OpenAIKey string `name:"openai-key" help:"OpenAI API key" env:"OPENAI_API_KEY"`

		// Storage configuration - Kong populates these from environment variables
		DatabaseURL    string `env:"DATABASE_URL" help:"PostgreSQL connection string"`
		R2Endpoint     string `env:"R2_ENDPOINT" help:"R2/S3 endpoint URL"`
		AWSRegion      string `env:"AWS_REGION" help:"AWS region"`
		R2Bucket       string `env:"R2_BUCKET" help:"R2/S3 bucket name"`
		AWSAccessKeyID string `env:"AWS_ACCESS_KEY_ID" help:"AWS access key ID"`
		AWSSecretKey   string `env:"AWS_SECRET_ACCESS_KEY" help:"AWS secret access key" `
		UsePathStyle   string `env:"USE_PATH_STYLE" help:"Use path-style S3 addressing"`

		storage storage.Storage `kong:"-"`
	}
	// Project1Cmd defines the command structure for running Project 1 grading.
	Project1Cmd struct {
		CommonProjectArgs
	}
	// Project2Cmd defines the command structure for running Project 2 grading.
	Project2Cmd struct {
		CommonProjectArgs
	}
	// CommonProjectArgs contains arguments shared across project grading commands.
	CommonProjectArgs struct {
		ServerURL string         `name:"server-url" default:"https://gradebot-unt-fab5dc5c.koyeb.app" help:"URL of the grading server"`
		Dir       client.WorkDir `name:"dir" help:"Path to your project directory (must exist and be accessible)" required:"" default:"."`
		RunCmd    string         `name:"run" help:"Command to run your program" required:""`

		Client *http.Client `kong:"-"`
		Stdin  io.Reader    `kong:"-"` // For testing - can inject stdin for prompts
		Stdout io.Writer    `kong:"-"` // For testing - can capture output
	}
)

// AfterApply is a Kong hook that initializes the HTTP client with the build ID.
func (c *CommonProjectArgs) AfterApply(_ Context, buildID string) error {
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: client.NewAuthTransport(buildID, http.DefaultTransport),
	}
	c.Client = httpClient
	return nil
}

// Context wraps context to get around reflection issues in Bind()
type Context struct {
	context.Context
}

// New creates and configures a new Kong context for the CLI application.
func New(ctx context.Context, name string, id [32]byte) *kong.Context {
	buildID := hex.EncodeToString(id[:])
	var cli CLI
	return kong.Parse(&cli,
		kong.Name(name),
		kong.UsageOnError(),
		kong.Bind(Context{ctx}, buildID),
	)
}

// AfterApply is a Kong hook that initializes the storage backend.
// Kong has already populated the env vars into the struct fields.
func (cmd *ServerCmd) AfterApply(ctx Context) error {
	var err error
	switch {
	case cmd.DatabaseURL != "":
		// SQL storage (PostgreSQL)
		if cmd.storage, err = storage.NewSQLStorage(ctx, cmd.DatabaseURL); err != nil {
			return fmt.Errorf("failed to initialize SQL storage: %w", err)
		}

	case cmd.R2Endpoint != "":
		// R2 storage (Cloudflare R2 / S3-compatible)
		usePathStyle, _ := strconv.ParseBool(cmd.UsePathStyle)
		if cmd.storage, err = storage.NewR2Storage(ctx, &storage.R2Config{
			Endpoint:        cmd.R2Endpoint,
			Region:          cmd.AWSRegion,
			Bucket:          cmd.R2Bucket,
			AccessKeyID:     cmd.AWSAccessKeyID,
			SecretAccessKey: cmd.AWSSecretKey,
			UsePathStyle:    usePathStyle,
		}); err != nil {
			return fmt.Errorf("failed to initialize R2 storage: %w", err)
		}

	default:
		return fmt.Errorf("no storage backend configured: set DATABASE_URL or R2_ENDPOINT")
	}

	return nil
}

// Run starts the grading server with the configured storage backend.
func (cmd *ServerCmd) Run(ctx Context, buildID string) error {
	return server.Start(ctx, server.Config{
		ID:   buildID,
		Port: cmd.Port,
		OpenAIClient: openai.NewClient(cmd.OpenAIKey, &http.Client{
			Timeout: 35 * time.Second, // Slightly longer than the API timeout
		}),
		Storage: cmd.storage,
	})
}

// AfterRun is a Kong hook that closes the storage connection after the server stops.
func (cmd *ServerCmd) AfterRun() error {
	if cmd.storage != nil {
		return cmd.storage.Close()
	}
	return nil
}

// Run executes the Project 1 grading client.
func (cmd *Project1Cmd) Run(ctx Context) error {
	cfg := &client.Config{
		ServerURL:     cmd.ServerURL,
		Dir:           cmd.Dir,
		RunCmd:        cmd.RunCmd,
		QualityClient: protoconnect.NewQualityServiceClient(cmd.Client, cmd.ServerURL),
		RubricClient:  protoconnect.NewRubricServiceClient(cmd.Client, cmd.ServerURL),
		Reader:        cmd.Stdin,
		Writer:        cmd.Stdout,
	}

	return client.ExecuteProject1(ctx, cfg)
}

// Run executes the Project 2 grading client.
func (cmd *Project2Cmd) Run(ctx Context) error {
	cfg := &client.Config{
		ServerURL:     cmd.ServerURL,
		Dir:           cmd.Dir,
		RunCmd:        cmd.RunCmd,
		QualityClient: protoconnect.NewQualityServiceClient(cmd.Client, cmd.ServerURL),
		RubricClient:  protoconnect.NewRubricServiceClient(cmd.Client, cmd.ServerURL),
		Reader:        cmd.Stdin,
		Writer:        cmd.Stdout,
	}

	return client.ExecuteProject2(ctx, cfg)
}
