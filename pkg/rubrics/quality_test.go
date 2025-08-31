package rubrics

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestLoadExcludeMatcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "ValidConfig",
			config: `patterns:
  - "*.log"
  - "tmp/"`,
			wantErr: false,
		},
		{
			name:    "InvalidYAML",
			config:  `invalid yaml`,
			wantErr: true,
		},
		{
			name:    "MissingPatterns",
			config:  `other: value`,
			wantErr: false, // Decodes successfully, patterns is empty
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(tc.config)},
			}
			_, err := LoadExcludeMatcher(fs)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoadFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  fstest.MapFS
		config  string
		wantErr bool
		wantLen int
	}{
		{
			name:    "NoFiles",
			source:  fstest.MapFS{},
			config:  `patterns: []`,
			wantErr: false,
			wantLen: 0,
		},
		{
			name: "SingleFile",
			source: fstest.MapFS{
				"main.go": &fstest.MapFile{Data: []byte("package main")},
			},
			config:  `patterns: []`,
			wantErr: false,
			wantLen: 1,
		},
		{
			name: "ExcludedFile",
			source: fstest.MapFS{
				"main.go":  &fstest.MapFile{Data: []byte("package main")},
				"test.log": &fstest.MapFile{Data: []byte("log")},
			},
			config: `patterns:
  - "*.log"`,
			wantErr: false,
			wantLen: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configFS := fstest.MapFS{
				"exclude.yaml": &fstest.MapFile{Data: []byte(tc.config)},
			}
			files, err := loadFiles(tc.source, configFS)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, files, tc.wantLen)
			}
		})
	}
}
