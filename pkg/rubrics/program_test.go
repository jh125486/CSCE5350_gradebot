package rubrics

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockCommander is a mock implementation of the Commander interface.
type MockCommander struct {
	mock.Mock
}

func (m *MockCommander) SetDir(dir string) {
	m.Called(dir)
}

func (m *MockCommander) SetStdin(stdin io.Reader) {
	m.Called(stdin)
}

func (m *MockCommander) SetStdout(stdout io.Writer) {
	m.Called(stdout)
}

func (m *MockCommander) SetStderr(stderr io.Writer) {
	m.Called(stderr)
}

func (m *MockCommander) Run() error {
	// When Run is called, we can simulate writing to the stdout/stderr buffers.
	args := m.Called()
	stdout := m.Calls[2].Arguments.Get(0).(io.Writer) // Brittle, but works for this test
	stderr := m.Calls[3].Arguments.Get(0).(io.Writer) // Brittle
	stdout.Write([]byte("mock output"))
	stderr.Write([]byte("mock error output"))
	return args.Error(0)
}

func (m *MockCommander) Start() error {
	// When Start is called, behave like Run for tests that start the process without waiting.
	args := m.Called()
	stdout := m.Calls[2].Arguments.Get(0).(io.Writer)
	stderr := m.Calls[3].Arguments.Get(0).(io.Writer)
	stdout.Write([]byte("mock output"))
	stderr.Write([]byte("mock error output"))
	return args.Error(0)
}

func (m *MockCommander) ProcessKill() error {
	args := m.Called()
	return args.Error(0)
}

// MockCommandFactory is a mock implementation of the CommandFactory interface.
type MockCommandFactory struct {
	mock.Mock
}

func (m *MockCommandFactory) New(name string, arg ...string) Commander {
	args := m.Called(name, arg)
	return args.Get(0).(Commander)
}

func TestProgram_Path(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		workDir string
		runCmd  string
	}{
		{name: "SimplePath", workDir: "/tmp/workdir", runCmd: "go"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog := NewProgram(tc.workDir, tc.runCmd, nil)
			assert.Equal(t, tc.workDir, prog.Path())
		})
	}
}

func TestProgram_Run(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func() (*Program, *MockCommandFactory, *MockCommander)
		args    []string
		wantErr bool
	}{
		{
			name: "SuccessfulRun",
			setup: func() (*Program, *MockCommandFactory, *MockCommander) {
				mockCmd := new(MockCommander)
				mockFactory := new(MockCommandFactory)
				mockFactory.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return NewProgram(".", "go", mockFactory), mockFactory, mockCmd
			},
			args:    []string{"run", "."},
			wantErr: false,
		},
		{
			name: "RunError",
			setup: func() (*Program, *MockCommandFactory, *MockCommander) {
				mockCmd := new(MockCommander)
				mockFactory := new(MockCommandFactory)
				runError := errors.New("command failed")
				mockFactory.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(runError)
				return NewProgram(".", "go", mockFactory), mockFactory, mockCmd
			},
			args:    []string{"run", "."},
			wantErr: true,
		},
		{
			name: "ChdirFails",
			setup: func() (*Program, *MockCommandFactory, *MockCommander) {
				return NewProgram("/a/path/that/most/definitely/does/not/exist", "go", nil), nil, nil
			},
			args:    []string{"run", "."},
			wantErr: true,
		},
		{
			name: "NoRunCommand",
			setup: func() (*Program, *MockCommandFactory, *MockCommander) {
				return NewProgram(".", "", nil), nil, nil
			},
			args:    []string{},
			wantErr: true,
		},
		{
			name: "NoFactory",
			setup: func() (*Program, *MockCommandFactory, *MockCommander) {
				return NewProgram(".", "go", nil), nil, nil
			},
			args:    []string{"run", "."},
			wantErr: false, // returns nil
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, factory, mockCmd := tc.setup()
			err := prog.Run(tc.args...)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if factory != nil {
				factory.AssertExpectations(t)
			}
			if mockCmd != nil {
				mockCmd.AssertExpectations(t)
			}
		})
	}
}

func TestProgram_Do(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{name: "SimpleIn", input: "test input"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog := NewProgram(".", "go", nil)
			outLines, errOutLines, err := prog.Do(tc.input)
			assert.NoError(t, err)
			var buf bytes.Buffer
			buf.WriteString(tc.input)
			assert.Equal(t, buf.String(), prog.in.String())
			// Ensure no output was produced by Do()
			assert.Equal(t, "", prog.out.String())
			assert.Equal(t, "", prog.errOut.String())
			// Also verify returned slices match buffers (should be empty)
			assert.Equal(t, 0, len(outLines))
			assert.Equal(t, 0, len(errOutLines))
		})
	}
}

func TestProgram_Kill(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		setup           func() (*Program, *MockCommandFactory, *MockCommander)
		expectKillError bool
	}{
		{
			name: "KillSuccessful",
			setup: func() (*Program, *MockCommandFactory, *MockCommander) {
				mockCmd := new(MockCommander)
				mockFactory := new(MockCommandFactory)
				mockFactory.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				mockCmd.On("ProcessKill").Return(nil)
				return NewProgram(".", "go", mockFactory), mockFactory, mockCmd
			},
			expectKillError: false,
		},
		{
			name: "KillFails",
			setup: func() (*Program, *MockCommandFactory, *MockCommander) {
				mockCmd := new(MockCommander)
				mockFactory := new(MockCommandFactory)
				killError := errors.New("kill failed")
				mockFactory.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				mockCmd.On("ProcessKill").Return(killError)
				return NewProgram(".", "go", mockFactory), mockFactory, mockCmd
			},
			expectKillError: true,
		},
		{
			name: "KillNoProcess",
			setup: func() (*Program, *MockCommandFactory, *MockCommander) {
				return NewProgram(".", "go", nil), nil, nil
			},
			expectKillError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, factory, mockCmd := tc.setup()
			// If the mock uses a factory, run to create the process.
			if factory != nil {
				_ = prog.Run("run", ".")
			}
			err := prog.Kill()
			if tc.expectKillError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if mockCmd != nil {
				mockCmd.AssertCalled(t, "ProcessKill")
			}
		})
	}
}
