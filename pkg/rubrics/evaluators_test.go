package rubrics_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
)

// kvStoreMock simulates a persistent key-value store and file creation for rubric tests
type kvStoreMock struct {
	store          map[string]string
	tempDir        string
	fileCreated    bool
	firstRunErr    error
	secondRunErr   error
	runCallCount   int
	doErr          error
	doCallCount    int
	killErr        error
	clearOnRestart bool
	failOnSecondDo bool
}

func newKVStoreMock(t *testing.T) *kvStoreMock {
	return &kvStoreMock{
		store:       make(map[string]string),
		tempDir:     t.TempDir(),
		fileCreated: true, // Default to creating files
	}
}

func (m *kvStoreMock) Path() string { return m.tempDir }
func (m *kvStoreMock) Run(args ...string) error {
	m.runCallCount++
	if m.runCallCount == 1 && m.firstRunErr != nil {
		return m.firstRunErr
	}
	if m.runCallCount == 2 && m.secondRunErr != nil {
		return m.secondRunErr
	}
	return nil
}

func (m *kvStoreMock) Kill() error {
	if m.clearOnRestart {
		m.store = make(map[string]string)
	}
	return m.killErr
}

func (m *kvStoreMock) Do(input string) ([]string, []string, error) {
	m.doCallCount++
	if m.doErr != nil {
		if m.failOnSecondDo && m.doCallCount == 2 {
			return nil, nil, m.doErr
		} else if !m.failOnSecondDo && m.doCallCount == 1 {
			return nil, nil, m.doErr
		}
	}
	tokens := strings.Fields(input)
	if len(tokens) < 2 {
		return []string{""}, []string{}, nil
	}
	switch tokens[0] {
	case "SET":
		if len(tokens) >= 3 {
			m.store[tokens[1]] = strings.Join(tokens[2:], " ")
			// Create the actual data.db file in the temp directory only if fileCreated is true
			if m.fileCreated {
				dataFilePath := filepath.Join(m.tempDir, rubrics.DataFileName)
				if err := os.WriteFile(dataFilePath, []byte("mock data"), 0o644); err != nil {
					return nil, nil, fmt.Errorf("failed to create data file: %w", err)
				}
			}
		}
		return []string{""}, []string{}, nil
	case "GET":
		val := m.store[tokens[1]]
		return []string{val}, []string{}, nil
	default:
		return []string{""}, []string{}, nil
	}
}

func TestEvaluateDataFileCreated(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success",
			setupMock: func(m *kvStoreMock) {
				// Default mock behavior creates file
			},
			wantPoints:     5,
			wantNoteSubstr: rubrics.DataFileName + " file created",
		},
		{
			name: "RunFails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "DoFails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("do failed")
				m.failOnSecondDo = false
			},
			wantPoints:     0,
			wantNoteSubstr: "SET failed",
		},
		{
			name: "StatFails",
			setupMock: func(m *kvStoreMock) {
				m.fileCreated = false
			},
			wantPoints:     0,
			wantNoteSubstr: rubrics.DataFileName + " file was not created",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			bag := make(rubrics.RunBag)
			mock := newKVStoreMock(t)

			// Apply test-specific setup
			tt.setupMock(mock)

			// Reset to ensure clean state
			if err := rubrics.Reset(mock); err != nil {
				t.Fatalf("Failed to reset: %v", err)
			}

			result := rubrics.EvaluateDataFileCreated(ctx, mock, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}

func TestEvaluatePersistenceAfterRestart(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success",
			setupMock: func(m *kvStoreMock) {
				// Default mock behavior
			},
			wantPoints:     5,
			wantNoteSubstr: "correct value",
		},
		{
			name: "RunFails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "DoFails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("do failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "SET failed",
		},
		{
			name: "KillFails",
			setupMock: func(m *kvStoreMock) {
				m.killErr = errors.New("kill failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Kill failed",
		},
		{
			name: "RestartFails",
			setupMock: func(m *kvStoreMock) {
				m.secondRunErr = errors.New("restart failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Restart failed",
		},
		{
			name: "GetFails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("get failed")
				m.failOnSecondDo = true
			},
			wantPoints:     0,
			wantNoteSubstr: "GET after restart failed",
		},
		{
			name: "ValueMismatch",
			setupMock: func(m *kvStoreMock) {
				m.clearOnRestart = true
			},
			wantPoints:     0,
			wantNoteSubstr: "did not return expected value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			bag := make(rubrics.RunBag)
			mock := newKVStoreMock(t)

			// Apply test-specific setup
			tt.setupMock(mock)

			// Reset to ensure clean state
			if err := rubrics.Reset(mock); err != nil {
				t.Fatalf("Failed to reset: %v", err)
			}

			result := rubrics.EvaluatePersistenceAfterRestart(ctx, mock, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}

func TestEvaluateNonexistentGet(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success",
			setupMock: func(m *kvStoreMock) {
				// Default mock behavior
			},
			wantPoints:     5,
			wantNoteSubstr: "Correctly handled",
		},
		{
			name: "RunFails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "DoFails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("do failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mock := newKVStoreMock(t)
			bag := make(rubrics.RunBag)

			// Apply test-specific setup
			tt.setupMock(mock)

			result := rubrics.EvaluateNonexistentGet(ctx, mock, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}

// simpleMockProgram implements model.ProgramRunner for EvaluateSetGet tests.
type resp func(rubrics.RunBag) (string, string, error)

// simpleMockProgram implements model.ProgramRunner for EvaluateSetGet tests.
type simpleMockProgram struct {
	bag       rubrics.RunBag
	responses []resp
	runErr    error
}

func (s *simpleMockProgram) Path() string             { return "." }
func (s *simpleMockProgram) Run(args ...string) error { return s.runErr }
func (s *simpleMockProgram) Do(in string) ([]string, []string, error) {
	if len(s.responses) > 0 {
		r := s.responses[0]
		s.responses = s.responses[1:]
		out, errOut, err := r(s.bag)
		if out == "EMPTY" {
			return []string{}, []string{errOut}, err
		}
		if out == "" {
			return []string{}, []string{errOut}, err
		}
		return []string{out}, []string{errOut}, err
	}
	return []string{}, []string{}, nil
}
func (s *simpleMockProgram) Kill() error { return nil }

func TestEvaluateSetGet_Table(t *testing.T) {
	tests := []struct {
		name           string
		responses      func(bag rubrics.RunBag) []resp
		runErr         error
		wantPoints     float64
		wantNoteSubstr string
		expectBagKey   require.ValueAssertionFunc
	}{
		{
			name: "Success",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil }, // SET
					func(rb rubrics.RunBag) (string, string, error) { return rb["key1"].(string), "", nil },
				}
			},
			runErr:         nil,
			wantPoints:     5,
			wantNoteSubstr: "Successfully set and retrieved",
			expectBagKey:   require.NotEmpty,
		},
		{
			name:           "RunFails",
			responses:      func(bag rubrics.RunBag) []resp { return nil },
			runErr:         errors.New("run failed"),
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
			expectBagKey:   require.Empty,
		},
		{
			name: "SetError",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", errors.New("set failed") },
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetMismatch",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) { return "wrong", "", nil },
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "Expected",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetError",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) { return "", "", errors.New("get failed") },
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetUnexpectedErrorOut",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) { return rb["key1"].(string), "pizza", nil },
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully set and retrieved",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "SetLogging",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "stdout", "stderr", nil },
					func(rb rubrics.RunBag) (string, string, error) { return rb["key1"].(string), "", nil },
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully set and retrieved",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetEmpty",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "GET did not return any output",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "SetWithStderr",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "set stderr", nil },
					func(rb rubrics.RunBag) (string, string, error) { return rb["key1"].(string), "", nil },
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully set and retrieved",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetNoOutput",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) { return "EMPTY", "", nil },
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "GET did not return any output",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetWithSpaces",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) { return " " + rb["key1"].(string) + " ", "", nil },
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully set and retrieved",
			expectBagKey:   require.NotEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Setup
			bag := make(rubrics.RunBag)
			prog := &simpleMockProgram{bag: bag, responses: tt.responses(bag), runErr: tt.runErr}
			// Test
			item := rubrics.EvaluateSetGet(t.Context(), prog, bag)
			// Assert
			// Points is the maximum for the rubric item; Awarded holds the
			// actually awarded points.
			assert.Equal(t, tt.wantPoints, item.Awarded)
			assert.Contains(t, item.Note, tt.wantNoteSubstr)
			tt.expectBagKey(t, bag["key1"], "key1 presence in bag")
		})
	}
}

// TestEvaluateOverwriteKey tests the EvaluateOverwriteKey function.
func TestEvaluateOverwriteKey(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success",
			setupMock: func(m *kvStoreMock) {
				// Default mock behavior
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully overwrote key",
		},
		{
			name: "RunFails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "DoFails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("do failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			program := newKVStoreMock(t)
			bag := make(rubrics.RunBag)

			// Apply test-specific setup
			tt.setupMock(program)

			result := rubrics.EvaluateOverwriteKey(ctx, program, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}

type MockProgramRunner struct{}

func (m *MockProgramRunner) Path() string {
	return "."
}

func (m *MockProgramRunner) Run(args ...string) error {
	return nil
}

func (m *MockProgramRunner) Do(input string) ([]string, []string, error) {
	return []string{input}, []string{}, nil
}

func (m *MockProgramRunner) Kill() error {
	return nil
}

func TestReset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupFunc func(string) error
		wantError bool
	}{
		{
			name: "Success - file exists and gets removed",
			setupFunc: func(tempDir string) error {
				// Create data.db file
				dataFile := filepath.Join(tempDir, rubrics.DataFileName)
				return os.WriteFile(dataFile, []byte("test"), 0644)
			},
			wantError: false,
		},
		{
			name: "Success - file doesn't exist",
			setupFunc: func(tempDir string) error {
				// Don't create any file
				return nil
			},
			wantError: false,
		},
		{
			name: "Error - file exists but can't be removed",
			setupFunc: func(tempDir string) error {
				// Create data.db file but make directory read-only
				dataFile := filepath.Join(tempDir, rubrics.DataFileName)
				if err := os.WriteFile(dataFile, []byte("test"), 0644); err != nil {
					return err
				}
				// Make directory read-only to prevent removal
				return os.Chmod(tempDir, 0555)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			mock := &kvStoreMock{tempDir: tempDir}

			// Setup test condition
			if err := tt.setupFunc(tempDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// Restore directory permissions after test
			defer func() {
				os.Chmod(tempDir, 0755)
			}()

			// Execute Reset
			err := rubrics.Reset(mock)

			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "failed to remove existing data.db")
			} else {
				assert.NoError(t, err)

				// Verify file was removed if it existed
				dataFile := filepath.Join(tempDir, rubrics.DataFileName)
				_, err := os.Stat(dataFile)
				assert.True(t, os.IsNotExist(err), "data.db should not exist after Reset")
			}
		})
	}
}
