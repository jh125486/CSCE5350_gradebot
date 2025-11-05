package rubrics

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
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
func (p *Program) Run(args ...string) (err error) {
	cmdName, cmdArgs := p.resolveCommand(args)
	if cmdName == "" {
		return fmt.Errorf("no run command configured")
	}

	restore, err := p.changeToWorkDir()
	if err != nil {
		return err
	}
	defer func() {
		if restoreErr := restore(); restoreErr != nil {
			err = restoreErr
		}
	}()

	err = p.startCommand(cmdName, cmdArgs)

	return err
}

func (p *Program) changeToWorkDir() (func() error, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine working directory: %w", err)
	}

	if err := os.Chdir(p.WorkDir); err != nil {
		return nil, err
	}

	return func() error {
		return os.Chdir(currentDir)
	}, nil
}

func (p *Program) resolveCommand(args []string) (cmdName string, cmdArgs []string) {
	switch {
	case len(args) == 0 && len(p.RunCmd) == 0:
		return "", nil
	case len(args) == 0:
		return p.RunCmd[0], copyArgs(p.RunCmd[1:])
	case len(p.RunCmd) == 0:
		return args[0], copyArgs(args[1:])
	default:
		return p.RunCmd[0], copyArgs(args)
	}
}

func (p *Program) startCommand(cmdName string, cmdArgs []string) error {
	if p.cmdFactory == nil {
		return nil
	}

	p.cmd = p.cmdFactory.New(cmdName, cmdArgs...)
	p.cmd.SetDir(p.WorkDir)

	stdinR, stdinW := p.stdinWriterFactory()
	p.stdinW = stdinW
	p.cmd.SetStdin(stdinR)
	p.cmd.SetStdout(&p.out)
	p.cmd.SetStderr(&p.errOut)

	return p.cmd.Start()
}

// Do sends input to the running program and returns captured output
func (p *Program) Do(in string) (stdout, stderr []string, err error) {
	p.captureInput(in)

	if err := p.sendToStdin(in); err != nil {
		return nil, nil, err
	}

	prevOutLen := p.out.Len()
	prevErrLen := p.errOut.Len()

	p.waitForOutput(prevOutLen, prevErrLen)

	outStr, errStr := p.latestOutput(prevOutLen, prevErrLen)
	return splitLines(outStr), splitLines(errStr), nil
}

func (p *Program) captureInput(in string) {
	p.in.Reset()
	p.in.WriteString(in)
}

func (p *Program) sendToStdin(in string) error {
	if p.stdinW == nil {
		return nil
	}
	_, err := p.stdinW.Write([]byte(in + "\n"))
	return err
}

func (p *Program) waitForOutput(prevOutLen, prevErrLen int) {
	if p.stdinW == nil {
		return
	}

	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		if p.out.Len() > prevOutLen || p.errOut.Len() > prevErrLen {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *Program) latestOutput(prevOutLen, prevErrLen int) (stdout, stderr string) {
	stdout = p.out.String()
	if prevOutLen < len(stdout) {
		stdout = stdout[prevOutLen:]
	} else {
		stdout = ""
	}

	stderr = p.errOut.String()
	if prevErrLen < len(stderr) {
		stderr = stderr[prevErrLen:]
	} else {
		stderr = ""
	}

	return stdout, stderr
}

func splitLines(s string) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func copyArgs(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// Kill terminates the running program process
func (p *Program) Kill() error {
	if p.cmd != nil {
		return p.cmd.ProcessKill()
	}
	return nil
}
