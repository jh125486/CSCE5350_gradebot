package rubrics

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bufbuild/connect-go"
	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"gopkg.in/yaml.v3"

	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
)

// EvaluateQuality implements the same behavior as the old gRPC client wrapper.
func EvaluateQuality(client connect.HTTPClient, serverURL, instructions string) Evaluator {
	c := protoconnect.NewQualityServiceClient(client, serverURL)

	return func(ctx context.Context, program ProgramRunner, _ RunBag) RubricItem {
		return evaluateQualityImpl(ctx, c, program, instructions)
	}
}

func evaluateQualityImpl(ctx context.Context, c protoconnect.QualityServiceClient, program ProgramRunner, instructions string) RubricItem {
	itemRubric := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "Quality",
			Note:    msg,
			Awarded: awarded,
			Points:  20,
		}
	}

	files, err := loadFiles(os.DirFS(program.Path()), configFS)
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

//go:embed exclude.yaml
var configFS embed.FS

func loadFiles(source, configFS fs.FS) ([]*pb.File, error) {
	// Load the exclude configuration.
	matcher, err := LoadExcludeMatcher(configFS)
	if err != nil {
		return nil, fmt.Errorf("failed to load exclude config: %w", err)
	}

	files := make([]*pb.File, 0)
	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Match the path using the path components and the isDir flag.
		if matcher.Match(strings.Split(path, "/"), d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if !d.IsDir() {
			content, err := fs.ReadFile(source, path)
			if err != nil {
				return err
			}
			files = append(files, &pb.File{
				Name:    path,
				Content: string(content),
			})
		}

		return nil
	}

	if err := fs.WalkDir(source, ".", walkFn); err != nil {
		return nil, err
	}

	return files, nil
}

// LoadExcludeMatcher loads the filter configuration from the specified file path
func LoadExcludeMatcher(fileSystem fs.FS) (gitignore.Matcher, error) {
	// Load the embedded configuration
	f, err := fileSystem.Open("exclude.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded config: %w", err)
	}
	defer f.Close()
	// Strict: expect a `patterns` key (gitignore-style). Fail decode if absent or invalid.
	var raw struct {
		Patterns []string `yaml:"patterns"`
	}
	if err := yaml.NewDecoder(f).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode exclude config YAML: %w", err)
	}

	// Parse patterns into gitignore.Patterns
	patterns := make([]gitignore.Pattern, 0, len(raw.Patterns))
	for _, p := range raw.Patterns {
		pp := filepath.ToSlash(strings.TrimSpace(p))
		patterns = append(patterns, gitignore.ParsePattern(pp, nil))
	}

	m := gitignore.NewMatcher(patterns)
	return m, nil
}
