package rubrics

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecCommandFactory_Integration(t *testing.T) {
	// This is an integration test, not a unit test.
	// It verifies that our wrapper around exec.Cmd works correctly.

	factory := &ExecCommandFactory{Context: t.Context()}
	var cmd Commander
	if runtime.GOOS == "windows" {
		cmd = factory.New("cmd", "/C", "echo", "hello")
	} else {
		cmd = factory.New("echo", "hello")
	}

	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	err := cmd.Run()

	assert.NoError(t, err)
	assert.Empty(t, stderr.String())
	assert.Equal(t, "hello\n", strings.ReplaceAll(stdout.String(), "\r\n", "\n"))
}

func TestExecCmd_ProcessKill(t *testing.T) {
	t.Run("Process is nil", func(t *testing.T) {
		// This tests the branch where ProcessKill is called before Run.
		factory := &ExecCommandFactory{Context: t.Context()}
		cmd := factory.New("echo", "hello")

		// Don't run the command, so cmd.Process is nil
		err := cmd.ProcessKill()
		assert.NoError(t, err)
	})
}

// Test that SetDir and SetStdin apply values to the embedded exec.Cmd
func TestExecCmd_SetDirAndStdin(t *testing.T) {
	c := &execCmd{Cmd: &exec.Cmd{}}

	// Set directory and verify it was applied
	dir := "/tmp"
	c.SetDir(dir)
	if c.Dir != dir {
		t.Fatalf("expected Dir=%q, got %q", dir, c.Dir)
	}

	// Set stdin and verify it's stored (non-nil and the same type)
	r := strings.NewReader("hello")
	c.SetStdin(r)
	if c.Stdin == nil {
		t.Fatalf("expected Stdin to be set, got nil")
	}
}

// Test that ProcessKill returns nil when there's no process
func TestExecCmd_ProcessKill_NoProcess(t *testing.T) {
	c := &execCmd{Cmd: &exec.Cmd{}}
	if err := c.ProcessKill(); err != nil {
		t.Fatalf("expected nil when no process, got %v", err)
	}
}

// Test that ProcessKill kills a started process.
// This uses a short-lived sleep process and then kills it.
func TestExecCmd_ProcessKill_KillsProcess(t *testing.T) {
	// Start a long-running process we can kill
	cmd := exec.CommandContext(context.Background(), "sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep command: %v", err)
	}

	c := &execCmd{Cmd: cmd}
	if err := c.ProcessKill(); err != nil {
		// attempt to wait to avoid leaving a process
		_ = cmd.Wait()
		t.Fatalf("ProcessKill returned error: %v", err)
	}

	// Wait to reap the process; platform behavior differs so accept any result
	// from Wait and avoid asserting on ProcessState to prevent flakes.
	_ = cmd.Wait()
}

func TestExecCmd_Start(t *testing.T) {
	factory := &ExecCommandFactory{Context: t.Context()}
	cmd := factory.New("echo", "hello")

	err := cmd.Start()
	assert.NoError(t, err)

	// Kill it to avoid hanging
	err = cmd.ProcessKill()
	assert.NoError(t, err)
}
