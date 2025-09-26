package rubrics_test

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/bufbuild/connect-go"
	"github.com/stretchr/testify/assert"

	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
)

// errorFS is a filesystem that can be configured to return errors
type errorFS struct {
	fstest.MapFS
	readFileError     bool
	readFileErrorPath string
}

func (e *errorFS) Open(name string) (fs.File, error) {
	if e.readFileError && name == e.readFileErrorPath {
		return nil, fmt.Errorf("simulated read error for %s", name)
	}
	return e.MapFS.Open(name)
}

func (e *errorFS) ReadFile(name string) ([]byte, error) {
	if e.readFileError && name == e.readFileErrorPath {
		return nil, fmt.Errorf("simulated read error for %s", name)
	}
	return fs.ReadFile(e.MapFS, name)
}

// walkDirErrorFS simulates filesystem walk errors
type walkDirErrorFS struct {
	fstest.MapFS
}

func (w *walkDirErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "baddir" {
		return nil, fmt.Errorf("simulated walk error for directory: %s", name)
	}
	return w.MapFS.ReadDir(name)
}

// configErrorFS simulates config file errors - but since the config is embedded,
// we need to test paths that can actually fail in loadFiles
type configErrorFS struct {
	fstest.MapFS
	walkError bool
}

func (c *configErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if c.walkError && name == "." {
		return nil, fmt.Errorf("simulated root directory read error")
	}
	return c.MapFS.ReadDir(name)
}

// specificErrorFS creates a filesystem that triggers specific walk errors
type specificErrorFS struct {
	fstest.MapFS
	triggerWalkErr bool
}

func (s *specificErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if s.triggerWalkErr && name == "." {
		// Return a directory entry that will cause walkErr != nil in walkFn
		return []fs.DirEntry{
			&fakeDirEntry{name: "error-file.go", isDir: false},
		}, nil
	}
	return s.MapFS.ReadDir(name)
}

func (s *specificErrorFS) Stat(name string) (fs.FileInfo, error) {
	if s.triggerWalkErr && name == "error-file.go" {
		return nil, fmt.Errorf("simulated stat error for error-file.go")
	}
	return s.MapFS.Stat(name)
}

func (s *specificErrorFS) Open(name string) (fs.File, error) {
	if s.triggerWalkErr && name == "error-file.go" {
		return nil, fmt.Errorf("simulated open error for error-file.go")
	}
	return s.MapFS.Open(name)
}

// realWalkErrorFS creates an actual walk error by implementing a broken ReadDir
type realWalkErrorFS struct {
	fstest.MapFS
	causeBrokenWalk bool
}

func (r *realWalkErrorFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if r.causeBrokenWalk && name == "." {
		// Return an error that will propagate as walkErr to the walkFn
		return nil, fmt.Errorf("simulated directory read error")
	}
	return r.MapFS.ReadDir(name)
}

// fakeDirEntry implements fs.DirEntry
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (f *fakeDirEntry) Name() string { return f.name }
func (f *fakeDirEntry) IsDir() bool  { return f.isDir }
func (f *fakeDirEntry) Type() fs.FileMode {
	if f.isDir {
		return fs.ModeDir
	}
	return 0
}
func (f *fakeDirEntry) Info() (fs.FileInfo, error) {
	return nil, fmt.Errorf("simulated info error")
}

// MockQualityServiceClient is a mock for testing quality evaluation
type MockQualityServiceClient struct {
	EvaluateResponse *pb.EvaluateCodeQualityResponse
	EvaluateError    error
}

func (m *MockQualityServiceClient) EvaluateCodeQuality(ctx context.Context, req *connect.Request[pb.EvaluateCodeQualityRequest]) (*connect.Response[pb.EvaluateCodeQualityResponse], error) {
	if m.EvaluateError != nil {
		return nil, m.EvaluateError
	}
	return connect.NewResponse(m.EvaluateResponse), nil
}

// mockProgramRunner is a mock for testing
type mockProgramRunner struct{}

func (m *mockProgramRunner) Path() string             { return "" }
func (m *mockProgramRunner) Run(args ...string) error { return nil }
func (m *mockProgramRunner) Do(stdin string) (stdout, stderr []string, err error) {
	return nil, nil, nil
}
func (m *mockProgramRunner) Kill() error { return nil }

func TestEvaluateQuality(t *testing.T) {
	// Create a config filesystem with exclude.yaml
	configFS := fstest.MapFS{
		"exclude.yaml": &fstest.MapFile{
			Data: []byte(`include_extensions:
  - ".go"
  - ".py"
  - ".js"
exclude_directories:
  - "node_modules"
  - ".git"
  - "vendor"
`),
		},
	}
	tests := []struct {
		name           string
		instructions   string
		mockClient     *MockQualityServiceClient
		sourceFS       fs.FS
		configFS       fs.FS
		expectedName   string
		expectedNote   string
		expectedPoints float64
		expectedError  string
	}{
		{
			name:         "Success",
			instructions: "Review this Go code",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 85,
					Feedback:     "Good code quality",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"main.go": &fstest.MapFile{Data: []byte("package main\n\nfunc main() { println(\"hello\") }")},
			},
			expectedName:   "Quality",
			expectedNote:   "Good code quality",
			expectedPoints: 17.0, // 85/100 * 20
		},
		{
			name:         "ClientError",
			instructions: "Review this code",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: nil,
				EvaluateError:    fmt.Errorf("API error"),
			},
			sourceFS: fstest.MapFS{
				"main.go": &fstest.MapFile{Data: []byte("package main\n\nfunc main() { println(\"hello\") }")},
			},
			expectedName:   "Quality",
			expectedNote:   "Connect call failed: API error",
			expectedPoints: 0,
		},
		{
			name:         "EmptyFilesystem",
			instructions: "Review this code",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 90,
					Feedback:     "Great code",
				},
				EvaluateError: nil,
			},
			sourceFS:       fstest.MapFS{}, // Empty filesystem - no Go files to process
			expectedName:   "Quality",
			expectedNote:   "Great code",
			expectedPoints: 18.0, // 90/100 * 20
		},
		{
			name:         "LoadFilesFileSystemError",
			instructions: "Review this code",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 90,
					Feedback:     "Great code",
				},
				EvaluateError: nil,
			},
			sourceFS: &errorFS{
				MapFS: fstest.MapFS{
					"main.go": &fstest.MapFile{Data: []byte("package main")},
				},
				readFileError:     true,
				readFileErrorPath: "main.go",
			},
			expectedName:   "Quality",
			expectedNote:   "Failed to prepare code for review:",
			expectedPoints: 0,
		},
		{
			name:         "BinaryFileSkipped",
			instructions: "Review this mixed code",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 75,
					Feedback:     "Decent code quality",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"main.go":   &fstest.MapFile{Data: []byte("package main\nfunc main() {}")},
				"binary.go": &fstest.MapFile{Data: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}}, // Valid .go extension but invalid UTF-8, should be skipped
			},
			expectedName:   "Quality",
			expectedNote:   "Decent code quality",
			expectedPoints: 15.0, // 75/100 * 20, only main.go should be processed (binary.go skipped by UTF-8 check)
		},
		{
			name:         "ExcludeDirectories",
			instructions: "Review this structured code",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 80,
					Feedback:     "Good structure",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"main.go":           &fstest.MapFile{Data: []byte("package main")},
				"src/lib.go":        &fstest.MapFile{Data: []byte("package lib")},
				"target/main.go":    &fstest.MapFile{Data: []byte("package target")},      // Should be excluded
				".git/config":       &fstest.MapFile{Data: []byte("git config")},          // Should be excluded
				"node_modules/x.js": &fstest.MapFile{Data: []byte("console.log('test')")}, // Should be excluded
			},
			expectedName:   "Quality",
			expectedNote:   "Good structure",
			expectedPoints: 16.0, // 80/100 * 20, only main.go and src/lib.go processed
		},
		{
			name:         "MixedFileTypes",
			instructions: "Review multiple languages",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 85,
					Feedback:     "Multi-language code",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"main.go":     &fstest.MapFile{Data: []byte("package main")},
				"app.py":      &fstest.MapFile{Data: []byte("print('hello')")},
				"lib.rs":      &fstest.MapFile{Data: []byte("fn main() {}")},
				"script.js":   &fstest.MapFile{Data: []byte("console.log('test')")},
				"config.yaml": &fstest.MapFile{Data: []byte("key: value")}, // Should be excluded
				"README":      &fstest.MapFile{Data: []byte("readme")},     // No extension, excluded
			},
			expectedName:   "Quality",
			expectedNote:   "Multi-language code",
			expectedPoints: 17.0, // 85/100 * 20, processes .go, .py, .rs, .js files
		},
		{
			name:         "WalkDirError",
			instructions: "Review code with walk error",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 90,
					Feedback:     "Good code",
				},
				EvaluateError: nil,
			},
			sourceFS: &walkDirErrorFS{
				MapFS: fstest.MapFS{
					"main.go":        &fstest.MapFile{Data: []byte("package main")},
					"baddir/file.go": &fstest.MapFile{Data: []byte("package baddir")},
				},
			},
			expectedName:   "Quality",
			expectedNote:   "Failed to prepare code for review:",
			expectedPoints: 0, // Error should result in 0 points
		},
		{
			name:         "ConfigLoadError",
			instructions: "Review code with config error",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 90,
					Feedback:     "Good code",
				},
				EvaluateError: nil,
			},
			sourceFS: &configErrorFS{
				MapFS: fstest.MapFS{
					"main.go": &fstest.MapFile{Data: []byte("package main")},
				},
				walkError: true,
			},
			expectedName:   "Quality",
			expectedNote:   "Failed to prepare code for review:",
			expectedPoints: 0, // Config error should result in 0 points
		},
		{
			name:         "FileWalkError",
			instructions: "Review code with specific walk error",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 90,
					Feedback:     "Good code",
				},
				EvaluateError: nil,
			},
			sourceFS: &specificErrorFS{
				MapFS: fstest.MapFS{
					"main.go": &fstest.MapFile{Data: []byte("package main")},
				},
				triggerWalkErr: true,
			},
			expectedName:   "Quality",
			expectedNote:   "Failed to prepare code for review:",
			expectedPoints: 0, // Walk error should result in 0 points
		},
		{
			name:         "MultipleFileTypesWithErrors",
			instructions: "Comprehensive file processing test",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 85,
					Feedback:     "Mixed code quality",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"main.go":       &fstest.MapFile{Data: []byte("package main\nfunc main() { println(\"hello\") }")},
				"lib.py":        &fstest.MapFile{Data: []byte("def hello(): print('world')")},
				"script.js":     &fstest.MapFile{Data: []byte("console.log('test');")},
				"app.rs":        &fstest.MapFile{Data: []byte("fn main() { println!(\"rust\"); }")},
				"config.toml":   &fstest.MapFile{Data: []byte("key = 'value'")},  // No extension in allowlist
				"binary.go":     &fstest.MapFile{Data: []byte{0xFF, 0xFE, 0x00}}, // Invalid UTF-8 but valid .go extension
				"empty.go":      &fstest.MapFile{Data: []byte("")},               // Empty but valid UTF-8
				"target/out.go": &fstest.MapFile{Data: []byte("package target")}, // Excluded directory
				"src/nested.go": &fstest.MapFile{Data: []byte("package nested")}, // Nested directory
			},
			expectedName:   "Quality",
			expectedNote:   "Mixed code quality",
			expectedPoints: 17.0, // 85/100 * 20, processes valid source files
		},
		{
			name:         "UTF8ValidationTest",
			instructions: "Test UTF-8 validation path",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 100, // Make it easier to calculate expected points
					Feedback:     "Only valid UTF-8 files processed",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"main.go":    &fstest.MapFile{Data: []byte("package main\nfunc main() { fmt.Println(\"Hello\") }")}, // Valid UTF-8
				"invalid.go": &fstest.MapFile{Data: []byte{0xFF, 0xFE, 0xFD}},                                       // Invalid UTF-8 - should be skipped
			},
			expectedName:   "Quality",
			expectedNote:   "Only valid UTF-8 files processed",
			expectedPoints: 20.0, // 100/100 * 20, only main.go should be processed
		},
		{
			name:         "WalkDirErrorTest",
			instructions: "Test WalkDir error handling",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 80,
					Feedback:     "Files processed",
				},
				EvaluateError: nil,
			},
			sourceFS: &walkDirErrorFS{
				MapFS: fstest.MapFS{
					"baddir/test.go": &fstest.MapFile{Data: []byte("package main")}, // This will trigger walkDirErrorFS error
				},
			},
			expectedName:  "Quality",
			expectedError: "simulated walk error",
		},
		{
			name:         "ConfigLoadError",
			instructions: "Test config load error handling",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 80,
					Feedback:     "Files processed",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"test.go": &fstest.MapFile{Data: []byte("package main")},
			},
			// Use a configFS that will cause loadFileFilterConfig to fail
			configFS:      fstest.MapFS{}, // Missing exclude.yaml
			expectedName:  "Quality",
			expectedError: "Failed to prepare code for review",
		},
		{
			name:         "ConfigYAMLError",
			instructions: "Test config YAML decode error",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 80,
					Feedback:     "Files processed",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"test.go": &fstest.MapFile{Data: []byte("package main")},
			},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte("invalid: yaml: content: [[[")}, // Invalid YAML
			},
			expectedName:  "Quality",
			expectedError: "Failed to prepare code for review",
		},
		{
			name:         "SingleFileSuccess",
			instructions: "Simple single file test",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 100,
					Feedback:     "Perfect code",
				},
				EvaluateError: nil,
			},
			sourceFS: fstest.MapFS{
				"perfect.go": &fstest.MapFile{Data: []byte("package main\n\n// Perfect Go code\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}")},
			},
			expectedName:   "Quality",
			expectedNote:   "Perfect code",
			expectedPoints: 20.0, // 100/100 * 20
		},
		{
			name:         "RealWalkError",
			instructions: "Test with actual walk directory error",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 90,
					Feedback:     "Good code",
				},
				EvaluateError: nil,
			},
			sourceFS: &realWalkErrorFS{
				MapFS: fstest.MapFS{
					"main.go": &fstest.MapFile{Data: []byte("package main")},
				},
				causeBrokenWalk: true,
			},
			expectedName:   "Quality",
			expectedNote:   "Failed to prepare code for review:",
			expectedPoints: 0, // Directory read error should result in 0 points
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testConfigFS := tt.configFS
			if testConfigFS == nil {
				testConfigFS = configFS // Use default configFS if not specified
			}
			evaluator := rubrics.EvaluateQuality(tt.mockClient, tt.sourceFS, testConfigFS, tt.instructions)
			result := evaluator(context.Background(), &mockProgramRunner{}, rubrics.RunBag{})

			assert.Equal(t, tt.expectedName, result.Name)
			assert.Equal(t, 20.0, result.Points)

			if tt.expectedError != "" {
				// For error cases, check that the note contains the error
				assert.Contains(t, result.Note, tt.expectedError)
				assert.Equal(t, 0.0, result.Awarded) // Error cases should have 0 points
			} else {
				assert.Equal(t, tt.expectedPoints, result.Awarded)
				assert.Contains(t, result.Note, tt.expectedNote)
			}
		})
	}
}
