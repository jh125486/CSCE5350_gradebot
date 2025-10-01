package client

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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

			if tc.skipOnWindows && runtime.GOOS == "windows" {
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
