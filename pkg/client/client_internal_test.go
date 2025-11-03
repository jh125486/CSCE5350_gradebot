package client

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const osWindows = "windows"

func TestWorkDirValidate(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name          string
		setup         func(t *testing.T) (string, func())
		wantErr       bool
		errContains   string
		skipOnWindows bool
	}

	cases := []testCase{
		{
			name: "empty path",
			setup: func(t *testing.T) (string, func()) {
				return "", nil
			},
			wantErr:     true,
			errContains: "not specified",
		},
		{
			name: "nonexistent path",
			setup: func(t *testing.T) (string, func()) {
				missing := filepath.Join(t.TempDir(), "does-not-exist")
				return missing, nil
			},
			wantErr:     true,
			errContains: "no such file or directory",
		},
		{
			name: "not a directory",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				file, err := os.CreateTemp(dir, "file")
				if err != nil {
					t.Fatalf("CreateTemp: %v", err)
				}
				if err := file.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
				return file.Name(), func() { _ = os.Remove(file.Name()) }
			},
			wantErr:     true,
			errContains: "not a directory",
		},
		{
			name: "open failure",
			setup: func(t *testing.T) (string, func()) {
				base := t.TempDir()
				restricted := filepath.Join(base, "restricted")
				if err := os.Mkdir(restricted, 0o700); err != nil {
					t.Fatalf("Mkdir: %v", err)
				}
				if err := os.Chmod(restricted, 0o100); err != nil {
					t.Fatalf("Chmod: %v", err)
				}
				return restricted, func() { _ = os.Chmod(restricted, 0o700) }
			},
			wantErr:       true,
			errContains:   "open",
			skipOnWindows: true,
		},
		{
			name: "success",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				return dir, nil
			},
		},
		{
			name: "success with contents",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "file"), []byte("data"), 0o600); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
				return dir, nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.skipOnWindows && runtime.GOOS == osWindows {
				t.Skip("directory permission semantics differ on Windows")
			}

			path, cleanup := tc.setup(t)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}

			err := WorkDir(path).Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error containing %q, got %v", tc.errContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected success, got: %v", err)
			}
		})
	}
}

func TestDirectoryErrorUnwrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "unwrap_returns_wrapped_error",
			err:  os.ErrPermission,
			want: os.ErrPermission,
		},
		{
			name: "unwrap_with_nil",
			err:  nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dirErr := &DirectoryError{
				Err: tt.err,
			}

			got := dirErr.Unwrap()
			if tt.want == nil {
				if got != nil {
					t.Errorf("Unwrap() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Errorf("Unwrap() = nil, want error")
				} else if !errors.Is(got, tt.want) {
					t.Errorf("Unwrap() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestDirectoryErrorGetPermissionHelp(t *testing.T) {
	t.Parallel()

	// Test by checking the Error() output which includes getPermissionHelp()
	dirErr := &DirectoryError{
		Err: os.ErrPermission,
	}

	errorMsg := dirErr.Error()

	// The error message should contain the base error
	if !strings.Contains(errorMsg, "permission denied") {
		t.Errorf("Error() should contain base error, got: %s", errorMsg)
	}

	// Since we can't change runtime.GOOS, test the current platform's help
	currentGOOS := runtime.GOOS
	switch currentGOOS {
	case "darwin":
		if !strings.Contains(errorMsg, "macOS") {
			t.Errorf("Error() should contain macOS help on darwin, got: %s", errorMsg)
		}
	case "windows":
		if !strings.Contains(errorMsg, "Windows") {
			t.Errorf("Error() should contain Windows help on windows, got: %s", errorMsg)
		}
	case "linux":
		if !strings.Contains(errorMsg, "Linux") {
			t.Errorf("Error() should contain Linux help on linux, got: %s", errorMsg)
		}
	default:
		if !strings.Contains(errorMsg, "Check directory permissions") {
			t.Errorf("Error() should contain generic help, got: %s", errorMsg)
		}
	}
}

func TestPromptForSubmission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reader string
		want   bool
	}{
		{
			name:   "user_answers_yes",
			reader: "y\n",
			want:   true,
		},
		{
			name:   "user_answers_no",
			reader: "n\n",
			want:   false,
		},
		{
			name:   "user_answers_yes_uppercase",
			reader: "Y\n",
			want:   true,
		},
		{
			name:   "user_answers_invalid",
			reader: "maybe\n",
			want:   false,
		},
		{
			name:   "read_error_eof",
			reader: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := promptForSubmission(strings.NewReader(tt.reader))

			if got != tt.want {
				t.Errorf("promptForSubmission() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPromptForSubmissionWithNilReader(t *testing.T) {
	// This test exercises the nil reader path which uses os.Stdin
	// We can't easily test actual stdin reading without complex setup,
	// but we can at least call it to ensure it doesn't panic
	t.Skip("Skipping nil reader test as it would hang waiting for stdin")
}

func TestNewAuthTransportWithNilBase(t *testing.T) {
	t.Parallel()

	// Test that nil base uses DefaultTransport
	at := NewAuthTransport("mytoken", nil)
	if at == nil {
		t.Fatal("NewAuthTransport returned nil")
	}
	if at.token != "mytoken" {
		t.Errorf("token = %v, want %v", at.token, "mytoken")
	}
	if at.base == nil {
		t.Error("base should not be nil when input is nil")
	}
	// Check it's not nil is enough - we can't compare http.DefaultTransport directly
}

func TestWorkDirString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dir  WorkDir
		want string
	}{
		{
			name: "simple_path",
			dir:  WorkDir("/tmp/test"),
			want: "/tmp/test",
		},
		{
			name: "empty_path",
			dir:  WorkDir(""),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.dir.String()
			if got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}
