package rubrics

import (
	"context"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/bufbuild/connect-go"
	"github.com/stretchr/testify/assert"

	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
)

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
type mockProgramRunner struct {
	path string
}

func (m *mockProgramRunner) Path() string {
	return m.path
}

func (m *mockProgramRunner) Run(args ...string) error {
	return nil
}

func (m *mockProgramRunner) Do(stdin string) ([]string, []string, error) {
	return nil, nil, nil
}

func (m *mockProgramRunner) Kill() error {
	return nil
}

func TestLoadFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   fstest.MapFS
		configFS fstest.MapFS
		wantErr  bool
		wantLen  int
	}{
		{
			name:   "NoFiles",
			source: fstest.MapFS{},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(`include_extensions: [".go"]`)},
			},
			wantErr: false,
			wantLen: 0,
		},
		{
			name: "SingleFile",
			source: fstest.MapFS{
				"main.go": &fstest.MapFile{Data: []byte("package main")},
			},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(`include_extensions: [".go"]`)},
			},
			wantErr: false,
			wantLen: 1,
		},
		{
			name: "ExcludedFile",
			source: fstest.MapFS{
				"main.go":  &fstest.MapFile{Data: []byte("package main")},
				"test.log": &fstest.MapFile{Data: []byte("log")},
			},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(`include_extensions: [".go"]`)},
			},
			wantErr: false,
			wantLen: 1,
		},
		{
			name: "AllowlistWithBinaryAndUnknown",
			source: fstest.MapFS{
				"main.rs":      &fstest.MapFile{Data: []byte("fn main() { println!(\"Hello\"); }")},
				"Cargo.toml":   &fstest.MapFile{Data: []byte("[package]\nname = \"test\"")},
				"binary_file":  &fstest.MapFile{Data: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}}, // Invalid UTF-8
				"unknown.xyz":  &fstest.MapFile{Data: []byte("unknown file type")},
				"script.py":    &fstest.MapFile{Data: []byte("print(\"hello\")")},
				"compiled.exe": &fstest.MapFile{Data: []byte{0xDE, 0xAD, 0xBE, 0xEF}},
			},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(`include_extensions: [".rs", ".py"]`)},
			},
			wantErr: false,
			wantLen: 2, // main.rs, script.py (Cargo.toml excluded - not source code)
		},
		{
			name: "ExcludeDirectories",
			source: fstest.MapFS{
				"main.go":           &fstest.MapFile{Data: []byte("package main")},
				"target/debug/app":  &fstest.MapFile{Data: []byte{0xDE, 0xAD, 0xBE, 0xEF}}, // binary
				"target/main.go":    &fstest.MapFile{Data: []byte("package main")},
				".git/config":       &fstest.MapFile{Data: []byte("git config")},
				"node_modules/x.js": &fstest.MapFile{Data: []byte("console.log('test')")},
				"src/lib.go":        &fstest.MapFile{Data: []byte("package lib")},
			},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(`include_extensions: [".go", ".js"]
exclude_directories: ["target", ".git", "node_modules"]`)},
			},
			wantErr: false,
			wantLen: 2, // main.go, src/lib.go (target, .git, node_modules excluded)
		},
		{
			name: "InvalidYAMLConfig",
			source: fstest.MapFS{
				"main.go": &fstest.MapFile{Data: []byte("package main")},
			},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(`invalid yaml: [`)},
			},
			wantErr: true,
			wantLen: 0,
		},
		{
			name: "MissingConfigFile",
			source: fstest.MapFS{
				"main.go": &fstest.MapFile{Data: []byte("package main")},
			},
			configFS: fstest.MapFS{}, // No exclude.yaml file
			wantErr:  true,
			wantLen:  0,
		},
		{
			name: "CaseInsensitiveExtensions",
			source: fstest.MapFS{
				"main.go": &fstest.MapFile{Data: []byte("package main")},
				"lib.GO":  &fstest.MapFile{Data: []byte("package lib")},
				"test.Py": &fstest.MapFile{Data: []byte("print('hello')")},
			},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(`include_extensions: [".go", ".py"]`)},
			},
			wantErr: false,
			wantLen: 3, // All should match case-insensitively
		},
		{
			name: "FileWithNoExtension",
			source: fstest.MapFS{
				"main.go":  &fstest.MapFile{Data: []byte("package main")},
				"README":   &fstest.MapFile{Data: []byte("readme content")},
				"Makefile": &fstest.MapFile{Data: []byte("all:\n\techo hello")},
			},
			configFS: fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(`include_extensions: [".go"]`)},
			},
			wantErr: false,
			wantLen: 1, // Only main.go, others have no extension
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files, err := loadFiles(tc.source, tc.configFS)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, files, tc.wantLen)
			}
		})
	}
}

func TestFileFilterConfig_ShouldIncludeFile(t *testing.T) {
	t.Parallel()

	config := &fileFilterConfig{
		IncludeExtensions: []string{".go", ".rs", ".py", ".JS"},
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "GoFile",
			path:     "main.go",
			expected: true,
		},
		{
			name:     "RustFile",
			path:     "src/lib.rs",
			expected: true,
		},
		{
			name:     "PythonFile",
			path:     "script.py",
			expected: true,
		},
		{
			name:     "JSFileCaseInsensitive",
			path:     "app.js",
			expected: true,
		},
		{
			name:     "JSFileUpperCase",
			path:     "APP.JS",
			expected: true,
		},
		{
			name:     "NoExtension",
			path:     "README",
			expected: false,
		},
		{
			name:     "UnknownExtension",
			path:     "data.txt",
			expected: false,
		},
		{
			name:     "BinaryFile",
			path:     "app.exe",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := config.ShouldIncludeFile(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestEvaluateQuality(t *testing.T) {
	tests := []struct {
		name           string
		instructions   string
		mockClient     *MockQualityServiceClient
		program        ProgramRunner
		expectedName   string
		expectedNote   string
		expectedPoints float64
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
			program:        &mockProgramRunner{path: "testdata"},
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
			program:        &mockProgramRunner{path: "testdata"},
			expectedName:   "Quality",
			expectedNote:   "Connect call failed: API error",
			expectedPoints: 0,
		},
		{
			name:         "LoadFilesError",
			instructions: "Review this code",
			mockClient: &MockQualityServiceClient{
				EvaluateResponse: &pb.EvaluateCodeQualityResponse{
					QualityScore: 90,
					Feedback:     "Great code",
				},
				EvaluateError: nil,
			},
			program:        &mockProgramRunner{path: "/nonexistent"},
			expectedName:   "Quality",
			expectedNote:   "Failed to prepare code for review:",
			expectedPoints: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator := EvaluateQuality(tt.mockClient, tt.instructions)
			result := evaluator(context.Background(), tt.program, RunBag{})

			assert.Equal(t, tt.expectedName, result.Name)
			assert.Equal(t, tt.expectedPoints, result.Awarded)
			assert.Equal(t, 20.0, result.Points)
			assert.Contains(t, result.Note, tt.expectedNote)
		})
	}
}
