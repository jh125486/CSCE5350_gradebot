package rubrics

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SafeBuffer is a thread-safe bytes.Buffer wrapper that uses a mutex to protect
// concurrent reads and writes. It is safe to use from multiple goroutines.
type SafeBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

// Write implements io.Writer with mutex protection.
func (sb *SafeBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

// Len returns the number of bytes in the buffer.
func (sb *SafeBuffer) Len() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Len()
}

// String returns the contents of the buffer as a string.
func (sb *SafeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

// Program implements the ProgramRunner interface using a CommandFactory
// to allow for testable command execution.
type Program struct {
	WorkDir    string
	RunCmd     []string
	cmdFactory CommandFactory

	cmd    Commander
	in     bytes.Buffer
	out    SafeBuffer
	errOut SafeBuffer
	stdinW io.WriteCloser

	// For testing: allow injection of stdin writer
	stdinWriterFactory func() (io.Reader, io.WriteCloser)
}

// NewProgram creates a new Program instance.
func NewProgram(workDir, runCmd string, factory CommandFactory) *Program {
	// Convert relative paths to absolute paths to avoid issues with os.Chdir
	if absDir, err := filepath.Abs(workDir); err == nil {
		workDir = absDir
	}

	return &Program{
		WorkDir:    workDir,
		RunCmd:     strings.Fields(runCmd),
		cmdFactory: factory,
		// Default stdin writer factory uses io.Pipe
		stdinWriterFactory: func() (io.Reader, io.WriteCloser) {
			return io.Pipe()
		},
	}
}

// SetStdinWriterFactory allows injection of a custom stdin writer factory for testing
func (p *Program) SetStdinWriterFactory(factory func() (io.Reader, io.WriteCloser)) {
	p.stdinWriterFactory = factory
}

// Update ProgramRunner interface to match new Do signature in types.go

// Path returns the working directory path
func (p *Program) Path() string { return p.WorkDir }

// Run starts the program with the given arguments
func (p *Program) Run(args ...string) error {
	d, err := os.Getwd()
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Chdir(d); err != nil {
			log.Printf("Failed to change directory back: %v", err)
		}
	}()
	if err := os.Chdir(p.WorkDir); err != nil {
		return err
	}

	// Determine command name and args. Prefer explicit args passed to Run,
	// otherwise fall back to the configured RunCmd slice. If neither is
	// provided, there's nothing to run.
	var cmdName string
	var cmdArgs []string

	// If a RunCmd was configured, use its first token as the command name and
	// its remaining tokens as default args.
	if len(p.RunCmd) > 0 {
		cmdName = p.RunCmd[0]
		if len(p.RunCmd) > 1 {
			cmdArgs = p.RunCmd[1:]
		}
	}

	// If explicit args were provided to Run, use them as the arguments. If no
	// command name has been determined yet, treat the first explicit arg as
	// the command name.
	if len(args) > 0 {
		if cmdName == "" {
			cmdName = args[0]
			if len(args) > 1 {
				cmdArgs = args[1:]
			}
		} else {
			cmdArgs = args
		}
	}

	if cmdName == "" {
		return fmt.Errorf("no run command configured")
	}

	// If we don't have a factory, we can't create a command. Return nil to
	// allow callers that don't require a process to continue (e.g., when
	// RunCmd is intentionally empty).
	if p.cmdFactory == nil {
		return nil
	}

	// Wire up the command and arguments
	p.cmd = p.cmdFactory.New(cmdName, cmdArgs...)
	p.cmd.SetDir(p.WorkDir)
	// Use the injected stdin writer factory for testability
	stdinR, stdinW := p.stdinWriterFactory()
	p.stdinW = stdinW
	p.cmd.SetStdin(stdinR)
	// SafeBuffer provides thread-safe writes from command goroutines
	p.cmd.SetStdout(&p.out)
	p.cmd.SetStderr(&p.errOut)

	// Start the command so callers can interact with it via Do(). If the
	// command implementation only supports Run (blocking), Run should still
	// be available; prefer Start when present.
	if err := p.cmd.Start(); err != nil {
		return err
	}
	return nil
}

// Do sends input to the running program and returns captured output
func (p *Program) Do(in string) (stdout, stderr []string, err error) {
	// Mirror input into buffer for test visibility
	p.in.Reset()
	p.in.WriteString(in)

	// If we have a running process, write the input (with newline) to stdin
	if p.stdinW != nil {
		if _, err := p.stdinW.Write([]byte(in + "\n")); err != nil {
			return nil, nil, err
		}
	}

	// Capture only new output since this call began
	prevOutLen := p.out.Len()
	prevErrLen := p.errOut.Len()

	// Only poll if we have a running process, otherwise return immediately
	if p.stdinW != nil {
		// Wait briefly for the program to produce output
		// We poll for growth for up to ~750ms
		deadline := time.Now().Add(750 * time.Millisecond)
		for time.Now().Before(deadline) {
			outLen := p.out.Len()
			errLen := p.errOut.Len()
			if outLen > prevOutLen || errLen > prevErrLen {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	outStr := p.out.String()
	errStr := p.errOut.String()
	if prevOutLen < len(outStr) {
		outStr = outStr[prevOutLen:]
	} else {
		outStr = ""
	}
	if prevErrLen < len(errStr) {
		errStr = errStr[prevErrLen:]
	} else {
		errStr = ""
	}

	var outLines, errLines []string
	scanner := bufio.NewScanner(strings.NewReader(outStr))
	for scanner.Scan() {
		outLines = append(outLines, scanner.Text())
	}
	scanner = bufio.NewScanner(strings.NewReader(errStr))
	for scanner.Scan() {
		errLines = append(errLines, scanner.Text())
	}
	return outLines, errLines, nil
}

// Kill terminates the running program process
func (p *Program) Kill() error {
	if p.cmd != nil {
		return p.cmd.ProcessKill()
	}
	return nil
}
