package rubrics

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/bufbuild/connect-go"
	"gopkg.in/yaml.v3"

	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
)

// EvaluateQuality implements the same behavior as the old gRPC client wrapper.
func EvaluateQuality(client protoconnect.QualityServiceClient, sourceFS, configFS fs.FS, instructions string) Evaluator {
	return func(ctx context.Context, _ ProgramRunner, _ RunBag) RubricItem {
		return evaluateQualityImpl(ctx, client, sourceFS, configFS, instructions)
	}
}

func evaluateQualityImpl(ctx context.Context, c protoconnect.QualityServiceClient, source, config fs.FS, instructions string) RubricItem {
	itemRubric := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "Quality",
			Note:    msg,
			Awarded: awarded,
			Points:  20,
		}
	}

	files, err := loadFiles(source, config)
	if err != nil {
		return itemRubric(fmt.Sprintf("Failed to prepare code for review: %v", err), 0)
	}

	req := connect.NewRequest(&pb.EvaluateCodeQualityRequest{
		Instructions: instructions,
		Files:        files,
	})
	resp, err := c.EvaluateCodeQuality(ctx, req)
	if err != nil {
		return itemRubric(fmt.Sprintf("Connect call failed: %v", err), 0)
	}
	awarded := float64(resp.Msg.QualityScore) / 100.0 * 20

	return itemRubric(resp.Msg.Feedback, awarded)
}

func loadFiles(source, configFS fs.FS) ([]*pb.File, error) {
	config, err := loadFileFilterConfig(configFS)
	if err != nil {
		return nil, fmt.Errorf("failed to load filter config: %w", err)
	}

	files := make([]*pb.File, 0)

	walkFn := func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if shouldSkipDir(path, entry, config.ExcludeDirectories) {
			return fs.SkipDir
		}

		if entry.IsDir() {
			return nil
		}

		if !config.shouldIncludeFile(path) {
			return nil
		}

		content, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}

		if !utf8.Valid(content) {
			return nil
		}

		files = append(files, &pb.File{
			Name:    path,
			Content: string(content),
		})
		return nil
	}

	if err := fs.WalkDir(source, ".", walkFn); err != nil {
		return nil, err
	}

	return files, nil
}

func shouldSkipDir(path string, entry fs.DirEntry, excludeDirs []string) bool {
	if !entry.IsDir() {
		return false
	}

	dirName := entry.Name()
	for _, excludeDir := range excludeDirs {
		if dirName == excludeDir || path == excludeDir {
			return true
		}
	}

	return false
}

// fileFilterConfig represents the configuration for including/excluding files
type fileFilterConfig struct {
	IncludeExtensions  []string `yaml:"include_extensions"`
	ExcludeDirectories []string `yaml:"exclude_directories"`
}

// shouldIncludeFile determines if a file should be included based on the config
func (c *fileFilterConfig) shouldIncludeFile(path string) bool {
	// Check if it has an extension we want to include
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false // No extension, not allowed for code quality
	}

	for _, includeExt := range c.IncludeExtensions {
		if strings.EqualFold(ext, includeExt) {
			return true
		}
	}

	return false
}

// loadFileFilterConfig loads the include/exclude configuration
func loadFileFilterConfig(configFS fs.FS) (*fileFilterConfig, error) {
	f, err := configFS.Open("exclude.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded config: %w", err)
	}
	defer f.Close()

	var config fileFilterConfig
	if err := yaml.NewDecoder(f).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode filter config YAML: %w", err)
	}

	return &config, nil
}
