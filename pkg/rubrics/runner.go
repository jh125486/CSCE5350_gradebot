package rubrics

import (
	"context"
	"io"
	"os/exec"
)

// Commander defines an interface that wraps an external command.
// This is a subset of exec.Cmd to allow for mocking.
type Commander interface {
	SetDir(dir string)
	SetStdin(stdin io.Reader)
	SetStdout(stdout io.Writer)
	SetStderr(stderr io.Writer)
	// Start begins execution without waiting for completion. Run may block
	// until the process exits.
	Start() error
	Run() error
	ProcessKill() error
}

// CommandFactory defines an interface for creating new commands.
type CommandFactory interface {
	New(name string, arg ...string) Commander
}

// execCmd is the production implementation of Commander, wrapping exec.Cmd.
type execCmd struct {
	*exec.Cmd
}

func (c *execCmd) SetDir(dir string) {
	c.Dir = dir
}

func (c *execCmd) SetStdin(stdin io.Reader) {
	c.Stdin = stdin
}

func (c *execCmd) SetStdout(stdout io.Writer) {
	c.Stdout = stdout
}

func (c *execCmd) SetStderr(stderr io.Writer) {
	c.Stderr = stderr
}

func (c *execCmd) ProcessKill() error {
	if c.Process != nil {
		return c.Process.Kill()
	}
	return nil
}

// Start starts the command without waiting for it to exit. This allows callers
// to interact with the process via stdin/stdout using the provided buffers.
func (c *execCmd) Start() error {
	return c.Cmd.Start()
}

// Run runs the command and waits for it to exit (start + wait). This mirrors
// the behavior of exec.Cmd.Run for callers that expect the command to
// complete before returning.
func (c *execCmd) Run() error {
	return c.Cmd.Run()
}

// ExecCommandFactory is the production implementation of CommandFactory.
type ExecCommandFactory struct {
	context.Context
}

// New creates a new execCmd wrapper.
//
//nolint:ireturn // Factory method intentionally returns interface
func (f *ExecCommandFactory) New(name string, arg ...string) Commander {
	return &execCmd{Cmd: exec.CommandContext(f.Context, name, arg...)}
}

// ProgramRunner is the interface used by rubrics to run student programs.
// It is declared here so runner-related abstractions live together.
type ProgramRunner interface {
	Path() string
	Run(args ...string) error
	Do(in string) ([]string, []string, error)
	Kill() error
}
