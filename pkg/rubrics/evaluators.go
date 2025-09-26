package rubrics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const DataFileName = "data.db"

// Reset removes any existing data.db to ensure clean state
func Reset(program ProgramRunner) error {
	dataFilePath := filepath.Join(program.Path(), DataFileName)
	if err := os.Remove(dataFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing data.db: %w", err)
	}
	return nil
}

// EvaluateDataFileCreated checks that data.db is created after a SET
func EvaluateDataFileCreated(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "DataFileCreated",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}
	bag[key1] = uuid.New().String()
	_, err := do(ctx, program, fmt.Sprintf("SET %s %v", key1, bag[key1]))
	if err != nil {
		return rubricItem(fmt.Sprintf("SET failed: %v", err), 0)
	}
	// Wait briefly for file creation
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(program.Path(), DataFileName)); err != nil {
		return rubricItem(DataFileName+" file was not created", 0)
	}

	return rubricItem(DataFileName+" file created after SET", 5)
}

// EvaluatePersistenceAfterRestart kills and restarts the program, then checks GET
func EvaluatePersistenceAfterRestart(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "PersistenceAfterRestart",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}
	bag[key1] = uuid.New().String()
	_, err := do(ctx, program, fmt.Sprintf("SET %s %v", key1, bag[key1]))
	if err != nil {
		return rubricItem(fmt.Sprintf("SET failed: %v", err), 0)
	}
	// Kill the program
	if err := program.Kill(); err != nil {
		return rubricItem(fmt.Sprintf("Kill failed: %v", err), 0)
	}
	// Restart the same instance
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Restart failed: %v", err), 0)
	}
	// Wait briefly for the program to load data
	time.Sleep(100 * time.Millisecond)
	out, err := do(ctx, program, fmt.Sprintf("GET %s", key1))
	if err != nil {
		return rubricItem(fmt.Sprintf("GET after restart failed: %v", err), 0)
	}
	if len(out) == 0 || strings.TrimSpace(out[0]) != bag[key1] {
		return rubricItem("GET after restart did not return expected value", 0)
	}

	return rubricItem("GET after restart returned correct value", 5)
}

// EvaluateNonexistentGet checks GET on a nonexistent key
func EvaluateNonexistentGet(ctx context.Context, program ProgramRunner, _ RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "NonexistentGet",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}
	out, err := do(ctx, program, "GET doesnotexist")
	if err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	for _, line := range out {
		if len(strings.TrimSpace(line)) > 2 {
			// Allow for some sort of prompt or minimal output.
			return rubricItem(fmt.Sprintf("Expected empty or error response, got '%s'", line), 0)
		}
	}

	return rubricItem("Correctly handled nonexistent key", 5)
}

const key1 = "key1"

// EvaluateSetGet evaluates basic SET and GET functionality
func EvaluateSetGet(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "SetGet",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	bag[key1] = uuid.New().String()

	// Do SET
	_, err := do(ctx, program, fmt.Sprintf("SET %s %v", key1, bag[key1]))
	if err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	// Do GET
	out, err := do(ctx, program, fmt.Sprintf("GET %s", key1))
	if err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	// Check GET - be flexible with prompt characters
	if len(out) == 0 {
		return rubricItem("GET did not return any output", 0)
	}

	expected := bag[key1].(string) // Type assert to string
	actual := strings.TrimSpace(out[0])

	// Option 1: Check if output ends with expected value (handles "> UUID" case)
	if strings.HasSuffix(actual, expected) {
		return rubricItem("Successfully set and retrieved key-value pair", 5)
	}

	// Option 2: Remove leading non-alphanumeric characters and check
	trimmed := strings.TrimLeftFunc(actual, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-'
	})
	if trimmed == expected {
		return rubricItem("Successfully set and retrieved key-value pair", 5)
	}

	// If neither approach works, show the actual output for debugging
	return rubricItem(fmt.Sprintf("Expected '%s', got '%s'", expected, actual), 0)
}

// EvaluateOverwriteKey evaluates the SET and GET functionality for overwriting a key.
func EvaluateOverwriteKey(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "OverwriteKey",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	// Do SET first
	bag[key1] = uuid.New().String()
	if _, err := do(ctx, program, fmt.Sprintf("SET %s %v", key1, bag[key1])); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	// Overwrite the key
	bag[key1] = uuid.New().String()
	if _, err := do(ctx, program, fmt.Sprintf("SET %s %v", key1, bag[key1])); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	// Do GET
	out, err := do(ctx, program, fmt.Sprintf("GET %s", key1))
	if err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	// Check GET - be flexible with prompt characters
	if len(out) == 0 {
		return rubricItem("GET did not return any output", 0)
	}

	expected := bag[key1].(string)
	actual := strings.TrimSpace(out[0])

	// Check if output ends with expected value (handles "> UUID" case)
	if strings.HasSuffix(actual, expected) {
		return rubricItem("Successfully overwrote key and retrieved new value", 5)
	}

	// Remove leading non-alphanumeric characters and check
	trimmed := strings.TrimLeftFunc(actual, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-'
	})
	if trimmed == expected {
		return rubricItem("Successfully overwrote key and retrieved new value", 5)
	}

	return rubricItem("GET did not return the expected value", 0)
}

func do(ctx context.Context, program ProgramRunner, cmd string) ([]string, error) {
	out, errOut, err := program.Do(cmd)
	if err != nil {
		return nil, err
	}
	if len(errOut) > 0 {
		slog.InfoContext(ctx, "Unexpected STDERR", slog.Any("output", errOut))
	}

	return out, nil
}
