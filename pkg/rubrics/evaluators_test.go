package rubrics_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
)

// Command constants for KV store operations
const (
	cmdSET    = "SET"
	cmdGET    = "GET"
	cmdDEL    = "DEL"
	cmdEXISTS = "EXISTS"
	cmdMSET   = "MSET"
	cmdMGET   = "MGET"
	cmdEXPIRE = "EXPIRE"
	cmdRANGE  = "RANGE"
	cmdBEGIN  = "BEGIN"
	cmdCOMMIT = "COMMIT"
	cmdABORT  = "ABORT"
)

// kvStoreMock simulates a persistent key-value store and file creation for rubric tests
type kvStoreMock struct {
	store            map[string]string
	tempDir          string
	fileCreated      bool
	firstRunErr      error
	secondRunErr     error
	runCallCount     int
	doErr            error
	doCallCount      int
	killErr          error
	clearOnRestart   bool
	failOnSecondDo   bool
	returnEmptyOnGet bool
	returnWrongOnGet bool
	customDoFunc     func(input string) ([]string, []string, error)
}

func newKVStoreMock(t *testing.T) *kvStoreMock {
	tempDir := t.TempDir()
	return &kvStoreMock{
		store:       make(map[string]string),
		tempDir:     tempDir,
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

func (m *kvStoreMock) Do(input string) (stdout, stderr []string, err error) {
	m.doCallCount++

	// Allow custom function to override default behavior
	if m.customDoFunc != nil {
		return m.customDoFunc(input)
	}

	if m.doErr != nil && !m.failOnSecondDo {
		return nil, nil, m.doErr
	}
	if m.failOnSecondDo && m.doCallCount == 2 {
		return nil, nil, errors.New("second do call failed")
	}
	tokens := strings.Fields(input)
	if len(tokens) < 1 {
		return []string{""}, []string{}, nil
	}

	cmd := tokens[0]
	switch cmd {
	case cmdSET:
		if len(tokens) >= 3 {
			m.store[tokens[1]] = strings.Join(tokens[2:], " ")
			// Simulate file creation - create the actual file for the test
			if m.fileCreated {
				// Create the data.db file in the temp directory for the stat check
				dbPath := filepath.Join(m.tempDir, rubrics.DataFileName)
				os.MkdirAll(m.tempDir, 0o755)
				f, createErr := os.Create(dbPath)
				if createErr == nil {
					f.Close()
				}
			}
		}
		return []string{""}, []string{}, nil
	case cmdGET:
		if m.returnEmptyOnGet {
			return []string{}, []string{}, nil
		}
		if m.returnWrongOnGet {
			return []string{"wrong-value-returned"}, []string{}, nil
		}
		if len(tokens) >= 2 {
			val := m.store[tokens[1]]
			return []string{val}, []string{}, nil
		}
		return []string{""}, []string{}, nil
	case cmdDEL:
		if len(tokens) >= 2 {
			if _, exists := m.store[tokens[1]]; exists {
				delete(m.store, tokens[1])
				return []string{"1"}, []string{}, nil
			}
			return []string{"0"}, []string{}, nil
		}
		return []string{"0"}, []string{}, nil
	case cmdEXISTS:
		if len(tokens) >= 2 {
			if _, exists := m.store[tokens[1]]; exists {
				return []string{"1"}, []string{}, nil
			}
			return []string{"0"}, []string{}, nil
		}
		return []string{"0"}, []string{}, nil
	case cmdMSET:
		// MSET key1 val1 key2 val2 ...
		for i := 1; i < len(tokens)-1; i += 2 {
			if i+1 < len(tokens) {
				m.store[tokens[i]] = tokens[i+1]
			}
		}
		return []string{""}, []string{}, nil
	case cmdMGET:
		// MGET key1 key2 key3 ... -> return values on separate lines
		var results []string
		for i := 1; i < len(tokens); i++ {
			val, exists := m.store[tokens[i]]
			if exists && val != "" {
				results = append(results, val)
			} else {
				results = append(results, "")
			}
		}
		return results, []string{}, nil
	case cmdEXPIRE:
		// EXPIRE key seconds -> return 1
		return []string{"1"}, []string{}, nil
	case cmdRANGE:
		// RANGE startKey endKey -> return key-value pairs
		var results []string
		// For simplicity, just return stored keys in order
		for k, v := range m.store {
			results = append(results, k+" "+v)
		}
		return results, []string{}, nil
	case cmdBEGIN, cmdCOMMIT, cmdABORT:
		return []string{""}, []string{}, nil
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
		{
			name: "ReturnsLongUnexpectedOutput",
			setupMock: func(m *kvStoreMock) {
				// Mock will return long string for GET
				m.store["doesnotexist"] = "this is a very long unexpected output string that should not be returned for nonexistent key"
			},
			wantPoints:     0,
			wantNoteSubstr: "Expected empty or error response",
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
func (s *simpleMockProgram) Do(in string) (stdout, stderr []string, err error) {
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
		{
			name: "GetWithPromptCharacters",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) { return "> " + rb["key1"].(string), "", nil },
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully set and retrieved",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetWithLeadingNonAlphanumeric",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) { return ">>> " + rb["key1"].(string), "", nil },
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully set and retrieved",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetWithLeadingSymbolsNoSuffix",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) {
						// Return with leading symbols but add trailing text so HasSuffix fails
						return ">>> " + rb["key1"].(string) + " (extra)", "", nil
					},
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "Expected",
			expectBagKey:   require.NotEmpty,
		},
		{
			name: "GetWithLeadingSymbolsOnlyTrimLeftWorks",
			responses: func(bag rubrics.RunBag) []resp {
				return []resp{
					func(rb rubrics.RunBag) (string, string, error) { return "", "", nil },
					func(rb rubrics.RunBag) (string, string, error) {
						// Return with leading symbols and space ONLY (no suffix match)
						// Add a newline after to prevent HasSuffix from matching
						return " >>>" + rb["key1"].(string) + "\n", "", nil
					},
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
		{
			name: "SecondSetFails",
			setupMock: func(m *kvStoreMock) {
				m.failOnSecondDo = true // Second SET (do call) will fail
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "GetReturnsEmptyOutput",
			setupMock: func(m *kvStoreMock) {
				m.returnEmptyOnGet = true
			},
			wantPoints:     0,
			wantNoteSubstr: "GET did not return any output",
		},
		{
			name: "GetReturnsActuallyWrongValue",
			setupMock: func(m *kvStoreMock) {
				m.returnWrongOnGet = true
			},
			wantPoints:     0,
			wantNoteSubstr: "GET did not return the expected value",
		},
		{
			name: "GetWithPromptCharacters",
			setupMock: func(m *kvStoreMock) {
				// Mock will return value with prompt, testing the HasSuffix path
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdGET {
						val := m.store[tokens[1]]
						return []string{"> " + val}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) >= 3 {
						m.store[tokens[1]] = tokens[2]
						return []string{""}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully overwrote key",
		},
		{
			name: "GetWithLeadingSymbols",
			setupMock: func(m *kvStoreMock) {
				// Mock will return value with leading symbols, testing TrimLeftFunc path
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdGET {
						val := m.store[tokens[1]]
						return []string{">>> " + val}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) >= 3 {
						m.store[tokens[1]] = tokens[2]
						return []string{""}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully overwrote key",
		},
		{
			name: "GetWithLeadingSymbolsOnlyTrimLeftWorks",
			setupMock: func(m *kvStoreMock) {
				// Return with leading symbols and space so HasSuffix fails but TrimLeftFunc works
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdGET {
						val := m.store[tokens[1]]
						return []string{" >>>" + val}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) >= 3 {
						m.store[tokens[1]] = tokens[2]
						return []string{""}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "Successfully overwrote key",
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

func (m *MockProgramRunner) Do(input string) (stdout, stderr []string, err error) {
	return []string{input}, []string{}, nil
}

func (m *MockProgramRunner) Kill() error {
	return nil
}

func TestReset(t *testing.T) {
	t.Parallel()

	t.Run("Success - file exists and gets removed", func(t *testing.T) {
		t.Parallel()
		mock := newKVStoreMock(t)
		// Create the data file
		dbPath := filepath.Join(mock.Path(), rubrics.DataFileName)
		err := os.WriteFile(dbPath, []byte("test"), 0o644)
		require.NoError(t, err)

		// Reset should remove it
		err = rubrics.Reset(mock)
		assert.NoError(t, err)

		// File should not exist
		_, err = os.Stat(dbPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("Success - file doesn't exist", func(t *testing.T) {
		t.Parallel()
		mock := newKVStoreMock(t)
		// Don't create file, just call Reset
		err := rubrics.Reset(mock)
		assert.NoError(t, err)
	})

	t.Run("Error - file exists but can't be removed", func(t *testing.T) {
		t.Parallel()
		mock := newKVStoreMock(t)
		dbPath := filepath.Join(mock.Path(), rubrics.DataFileName)

		// Create the file
		err := os.WriteFile(dbPath, []byte("test"), 0o644)
		require.NoError(t, err)

		// Make directory read-only to prevent file removal
		err = os.Chmod(mock.Path(), 0o444)
		require.NoError(t, err)

		// Reset should fail
		err = rubrics.Reset(mock)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to remove existing data.db")

		// Restore permissions for cleanup
		os.Chmod(mock.Path(), 0o755)
	})
}

// TestEvaluateDeleteExists_Detailed provides comprehensive coverage
func TestEvaluateDeleteExists_Detailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success",
			setupMock: func(m *kvStoreMock) {
				// Default mock handles all commands correctly
			},
			wantPoints:     5,
			wantNoteSubstr: "correctly",
		},
		{
			name: "Run fails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "SET fails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("set failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "SET failed",
		},
		{
			name: "First EXISTS returns wrong value",
			setupMock: func(m *kvStoreMock) {
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdEXISTS {
						return []string{"0"}, []string{}, nil // Wrong: should be "1"
					}
					if len(tokens) > 0 && tokens[0] == cmdSET {
						return []string{""}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "EXISTS returned wrong value",
		},
		{
			name: "DEL returns wrong value",
			setupMock: func(m *kvStoreMock) {
				callCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdDEL {
						return []string{"0"}, []string{}, nil // Wrong: should be "1"
					}
					if len(tokens) > 0 && tokens[0] == cmdEXISTS {
						callCount++
						if callCount == 1 {
							return []string{"1"}, []string{}, nil
						}
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "DEL returned wrong value",
		},
		{
			name: "EXISTS after DEL returns wrong value",
			setupMock: func(m *kvStoreMock) {
				callCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == "EXISTS" {
						callCount++
						if callCount == 2 {
							return []string{"1"}, []string{}, nil // Wrong: should be "0"
						}
						return []string{"1"}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == "DEL" {
						return []string{"1"}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "EXISTS after DEL returned wrong value",
		},
		{
			name: "GET after DEL returns non-nil value",
			setupMock: func(m *kvStoreMock) {
				callCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdGET {
						return []string{"some-value"}, []string{}, nil // Wrong: should be empty
					}
					if len(tokens) > 0 && tokens[0] == cmdEXISTS {
						callCount++
						if callCount == 1 {
							return []string{"1"}, []string{}, nil
						}
						return []string{"0"}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdDEL {
						return []string{"1"}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "GET after DEL should return nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bag := make(rubrics.RunBag)
			mock := newKVStoreMock(t)
			tt.setupMock(mock)

			result := rubrics.EvaluateDeleteExists(ctx, mock, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}

// TestEvaluateMSetMGet_Detailed provides comprehensive coverage
func TestEvaluateMSetMGet_Detailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success",
			setupMock: func(m *kvStoreMock) {
				// Default mock handles MSET/MGET correctly
			},
			wantPoints:     5,
			wantNoteSubstr: "correctly",
		},
		{
			name: "Run fails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "MSET fails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("mset failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "MSET failed",
		},
		{
			name: "MGET returns too few lines",
			setupMock: func(m *kvStoreMock) {
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdMGET {
						return []string{"val1", "val2"}, []string{}, nil // Only 2 instead of 3
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "MGET returned too few lines",
		},
		{
			name: "MGET first value wrong",
			setupMock: func(m *kvStoreMock) {
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdMGET {
						// Return wrong values that won't match the UUIDs
						return []string{"wrong1", "wrong2", ""}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "returned wrong value",
		},
		{
			name: "MGET third value not nil",
			setupMock: func(m *kvStoreMock) {
				var capturedKeys []string
				var capturedVals []string
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == "MSET" {
						// Capture keys and values from MSET
						for i := 1; i < len(tokens)-1; i += 2 {
							if i+1 < len(tokens) {
								capturedKeys = append(capturedKeys, tokens[i])
								capturedVals = append(capturedVals, tokens[i+1])
							}
						}
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == "MGET" {
						// Return values for keys requested in MGET
						// MGET queries: keyB, keyA, keyZ (where keyZ doesn't exist)
						var results []string
						for i := 1; i < len(tokens); i++ {
							found := false
							for j, k := range capturedKeys {
								if k == tokens[i] {
									results = append(results, capturedVals[j])
									found = true
									break
								}
							}
							if !found {
								// This is keyZ - return a value instead of empty/nil
								results = append(results, "WRONG-VALUE")
							}
						}
						return results, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "should return nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bag := make(rubrics.RunBag)
			mock := newKVStoreMock(t)
			tt.setupMock(mock)

			result := rubrics.EvaluateMSetMGet(ctx, mock, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}

// TestEvaluateTTLBasic_Detailed provides comprehensive coverage
func TestEvaluateTTLBasic_Detailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success - key expires",
			setupMock: func(m *kvStoreMock) {
				var keySet string
				getCallCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) > 2 {
						keySet = tokens[2]
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdEXPIRE {
						return []string{"1"}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdGET {
						getCallCount++
						if getCallCount == 1 {
							return []string{keySet}, []string{}, nil // Before expiry
						}
						return []string{""}, []string{}, nil // After expiry
					}
					if len(tokens) > 0 && tokens[0] == "TTL" {
						return []string{"-2"}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "correctly",
		},
		{
			name: "Run fails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "SET fails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("set failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "SET failed",
		},
		{
			name: "EXPIRE returns wrong value",
			setupMock: func(m *kvStoreMock) {
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdEXPIRE {
						return []string{"0"}, []string{}, nil // Wrong: should be "1"
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "EXPIRE should return 1",
		},
		{
			name: "GET before expiry returns empty",
			setupMock: func(m *kvStoreMock) {
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdEXPIRE {
						return []string{"1"}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdGET {
						return []string{""}, []string{}, nil // Wrong: should have value
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "returned wrong value",
		},
		{
			name: "GET after expiry still has value",
			setupMock: func(m *kvStoreMock) {
				var keySet string
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) > 2 {
						keySet = tokens[2]
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdEXPIRE {
						return []string{"1"}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdGET {
						return []string{keySet}, []string{}, nil // Wrong: should be empty after expiry
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "should return nil",
		},
		{
			name: "TTL returns wrong value",
			setupMock: func(m *kvStoreMock) {
				var keySet string
				getCallCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) > 2 {
						keySet = tokens[2]
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdEXPIRE {
						return []string{"1"}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdGET {
						getCallCount++
						if getCallCount == 1 {
							return []string{keySet}, []string{}, nil
						}
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == "TTL" {
						return []string{"100"}, []string{}, nil // Wrong: should be "-2"
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "TTL should return -2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bag := make(rubrics.RunBag)
			mock := newKVStoreMock(t)
			tt.setupMock(mock)

			result := rubrics.EvaluateTTLBasic(ctx, mock, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}

// TestEvaluateRange_Detailed provides comprehensive coverage
func TestEvaluateRange_Detailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success - all ranges work",
			setupMock: func(m *kvStoreMock) {
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdRANGE {
						// Simulate correct RANGE output for different queries
						if strings.Contains(input, "RANGE b d") {
							return []string{"b", "c", "d", "END"}, []string{}, nil
						}
						if strings.Contains(input, `RANGE "" c`) || strings.Contains(input, "RANGE  c") {
							return []string{"a", "b", "c", "END"}, []string{}, nil
						}
						if strings.Contains(input, `RANGE d ""`) || strings.Contains(input, "RANGE d ") {
							return []string{"d", "e", "END"}, []string{}, nil
						}
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "correctly",
		},
		{
			name: "Run fails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "MSET fails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("mset failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "MSET failed",
		},
		{
			name: "First RANGE returns wrong keys",
			setupMock: func(m *kvStoreMock) {
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdRANGE {
						return []string{"a", "b", "END"}, []string{}, nil // Wrong: should be b, c, d
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "returned wrong keys",
		},
		{
			name: "Second RANGE fails",
			setupMock: func(m *kvStoreMock) {
				rangeCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == "RANGE" {
						rangeCount++
						if rangeCount == 1 {
							return []string{"b", "c", "d", "END"}, []string{}, nil
						}
						return nil, nil, errors.New("range failed")
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "failed",
		},
		{
			name: "Third RANGE returns wrong keys",
			setupMock: func(m *kvStoreMock) {
				rangeCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == "RANGE" {
						rangeCount++
						if rangeCount == 1 {
							return []string{"b", "c", "d", "END"}, []string{}, nil
						}
						if rangeCount == 2 {
							return []string{"a", "b", "c", "END"}, []string{}, nil
						}
						return []string{"x", "y", "END"}, []string{}, nil // Wrong
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "returned wrong keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bag := make(rubrics.RunBag)
			mock := newKVStoreMock(t)
			tt.setupMock(mock)

			result := rubrics.EvaluateRange(ctx, mock, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}

// TestEvaluateTransactions_Detailed provides comprehensive coverage
func TestEvaluateTransactions_Detailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name           string
		setupMock      func(*kvStoreMock)
		wantPoints     float64
		wantNoteSubstr string
	}{
		{
			name: "Success - full transaction flow",
			setupMock: func(m *kvStoreMock) {
				var commitKey, commitVal string
				inTransaction := false
				var txnStore = make(map[string]string)
				getCount := 0

				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) == 0 {
						return []string{""}, []string{}, nil
					}

					cmd := tokens[0]
					switch cmd {
					case cmdBEGIN:
						inTransaction = true
						txnStore = make(map[string]string)
						return []string{""}, []string{}, nil
					case cmdSET:
						if len(tokens) <= 2 {
							return []string{""}, []string{}, nil
						}
						key := tokens[1]
						val := tokens[2]
						if inTransaction {
							txnStore[key] = val
						}
						if commitKey == "" && getCount > 0 {
							// Second SET is for commit
							commitKey = key
							commitVal = val
						}
						return []string{""}, []string{}, nil
					case cmdGET:
						if len(tokens) <= 1 {
							return []string{""}, []string{}, nil
						}
						getCount++
						key := tokens[1]
						switch getCount {
						case 1:
							// First GET (in transaction, read-your-writes from txnStore)
							if val, ok := txnStore[key]; ok {
								return []string{val}, []string{}, nil
							}
							return []string{""}, []string{}, nil
						case 2:
							// After ABORT - key should not exist
							return []string{""}, []string{}, nil
						case 3:
							// After restart - committed value should persist
							if key == commitKey {
								return []string{commitVal}, []string{}, nil
							}
							return []string{""}, []string{}, nil
						}
					case cmdABORT:
						inTransaction = false
						txnStore = make(map[string]string)
						return []string{""}, []string{}, nil
					case cmdCOMMIT:
						inTransaction = false
						// Persist txnStore to main store
						return []string{""}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     5,
			wantNoteSubstr: "correctly",
		},
		{
			name: "Run fails",
			setupMock: func(m *kvStoreMock) {
				m.firstRunErr = errors.New("run failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Execution failed",
		},
		{
			name: "BEGIN fails",
			setupMock: func(m *kvStoreMock) {
				m.doErr = errors.New("begin failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "BEGIN failed",
		},
		{
			name: "GET in transaction returns wrong value",
			setupMock: func(m *kvStoreMock) {
				var txnVals = make(map[string]string)
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) > 2 {
						txnVals[tokens[1]] = tokens[2]
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdGET && len(tokens) > 1 {
						// Return empty instead of the actual value
						return []string{""}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "GET in transaction should return",
		},
		{
			name: "ABORT fails",
			setupMock: func(m *kvStoreMock) {
				var setVal string
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) > 2 {
						setVal = tokens[2]
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdABORT {
						return nil, nil, errors.New("abort failed")
					}
					if len(tokens) > 0 && tokens[0] == cmdGET {
						return []string{setVal}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "ABORT failed",
		},
		{
			name: "GET after ABORT still returns value",
			setupMock: func(m *kvStoreMock) {
				var setVal string
				getCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) > 2 {
						setVal = tokens[2]
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdGET {
						getCount++
						// Both GETs return value - second should be empty after abort
						return []string{setVal}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "GET after ABORT should return nil",
		},
		{
			name: "COMMIT fails",
			setupMock: func(m *kvStoreMock) {
				var firstSetVal, secondSetVal string
				getCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) > 2 {
						if firstSetVal == "" {
							firstSetVal = tokens[2]
						} else if secondSetVal == "" {
							secondSetVal = tokens[2]
						}
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdCOMMIT {
						return nil, nil, errors.New("commit failed")
					}
					if len(tokens) > 0 && tokens[0] == cmdGET {
						getCount++
						if getCount == 1 {
							return []string{firstSetVal}, []string{}, nil
						}
						if getCount == 2 {
							return []string{""}, []string{}, nil
						}
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "COMMIT failed",
		},
		{
			name: "Kill fails",
			setupMock: func(m *kvStoreMock) {
				var firstSetVal, commitVal string
				getCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == cmdSET && len(tokens) > 2 {
						if firstSetVal == "" {
							firstSetVal = tokens[2]
						} else if commitVal == "" {
							commitVal = tokens[2]
						}
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == cmdGET {
						getCount++
						if getCount == 1 {
							return []string{firstSetVal}, []string{}, nil
						}
						if getCount == 2 {
							return []string{""}, []string{}, nil
						}
					}
					return []string{""}, []string{}, nil
				}
				m.killErr = errors.New("kill failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Kill failed",
		},
		{
			name: "Restart fails",
			setupMock: func(m *kvStoreMock) {
				var firstSetVal, commitVal string
				getCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == "SET" && len(tokens) > 2 {
						if firstSetVal == "" {
							firstSetVal = tokens[2]
						} else if commitVal == "" {
							commitVal = tokens[2]
						}
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == "GET" {
						getCount++
						if getCount == 1 {
							return []string{firstSetVal}, []string{}, nil
						}
						if getCount == 2 {
							return []string{""}, []string{}, nil
						}
					}
					return []string{""}, []string{}, nil
				}
				m.secondRunErr = errors.New("restart failed")
			},
			wantPoints:     0,
			wantNoteSubstr: "Restart failed",
		},
		{
			name: "GET after restart returns empty (not persistent)",
			setupMock: func(m *kvStoreMock) {
				var firstSetVal string
				getCount := 0
				m.customDoFunc = func(input string) ([]string, []string, error) {
					tokens := strings.Fields(input)
					if len(tokens) > 0 && tokens[0] == "SET" && len(tokens) > 2 && firstSetVal == "" {
						firstSetVal = tokens[2]
						return []string{""}, []string{}, nil
					}
					if len(tokens) > 0 && tokens[0] == "GET" {
						getCount++
						if getCount == 1 {
							return []string{firstSetVal}, []string{}, nil
						}
						if getCount == 2 {
							return []string{""}, []string{}, nil
						}
						// After restart - should return empty (not persistent)
						return []string{""}, []string{}, nil
					}
					return []string{""}, []string{}, nil
				}
			},
			wantPoints:     0,
			wantNoteSubstr: "GET after restart should return",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bag := make(rubrics.RunBag)
			mock := newKVStoreMock(t)
			tt.setupMock(mock)

			result := rubrics.EvaluateTransactions(ctx, mock, bag)

			assert.Equal(t, tt.wantPoints, result.Awarded)
			assert.Contains(t, result.Note, tt.wantNoteSubstr)
		})
	}
}
