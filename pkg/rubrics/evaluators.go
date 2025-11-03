package rubrics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
)

const (
	DataFileName = "data.db"
	// expiryCheckDelay is the time to wait for key expiration to take effect.
	// EXPIRE command in tests uses 100ms, so we add a small buffer to ensure expiration.
	expiryCheckDelay = 110 * time.Millisecond
	// restartLoadDelay is the time to wait for a program to load persisted data after restart.
	restartLoadDelay = 100 * time.Millisecond
)

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
		contextlog.From(ctx).Info("Unexpected STDERR", "output", errOut)
	}

	return out, nil
}

// EvaluateDeleteExists checks DEL and EXISTS commands
func EvaluateDeleteExists(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "DeleteExists",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	key := uuid.New().String()
	value := uuid.New().String()
	bag["delExists_key"] = key

	// SET key value
	if _, err := do(ctx, program, fmt.Sprintf("SET %s %s", key, value)); err != nil {
		return rubricItem(fmt.Sprintf("SET failed: %v", err), 0)
	}

	// EXISTS key -> expect "1"
	out, err := do(ctx, program, fmt.Sprintf("EXISTS %s", key))
	if err != nil {
		return rubricItem(fmt.Sprintf("EXISTS failed: %v", err), 0)
	}
	if len(out) == 0 || !strings.Contains(strings.TrimSpace(out[0]), "1") {
		return rubricItem(fmt.Sprintf("EXISTS returned wrong value, expected '1', got '%v'", out), 0)
	}

	// DEL key -> expect "1"
	out, err = do(ctx, program, fmt.Sprintf("DEL %s", key))
	if err != nil {
		return rubricItem(fmt.Sprintf("DEL failed: %v", err), 0)
	}
	if len(out) == 0 || !strings.Contains(strings.TrimSpace(out[0]), "1") {
		return rubricItem(fmt.Sprintf("DEL returned wrong value, expected '1', got '%v'", out), 0)
	}

	// EXISTS key -> expect "0"
	out, err = do(ctx, program, fmt.Sprintf("EXISTS %s", key))
	if err != nil {
		return rubricItem(fmt.Sprintf("EXISTS after DEL failed: %v", err), 0)
	}
	if len(out) == 0 || !strings.Contains(strings.TrimSpace(out[0]), "0") {
		return rubricItem(fmt.Sprintf("EXISTS after DEL returned wrong value, expected '0', got '%v'", out), 0)
	}

	// GET key -> expect "nil" or empty
	out, err = do(ctx, program, fmt.Sprintf("GET %s", key))
	if err != nil {
		return rubricItem(fmt.Sprintf("GET after DEL failed: %v", err), 0)
	}
	if len(out) > 0 {
		trimmed := strings.TrimSpace(out[0])
		if trimmed != "" && !strings.Contains(strings.ToLower(trimmed), "nil") {
			return rubricItem(fmt.Sprintf("GET after DEL should return nil, got '%s'", trimmed), 0)
		}
	}

	return rubricItem("DEL and EXISTS work correctly", 5)
}

// EvaluateMSetMGet checks MSET and MGET commands
func EvaluateMSetMGet(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "MSetMGet",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	keyA := uuid.New().String()
	keyB := uuid.New().String()
	keyX := uuid.New().String()
	keyZ := uuid.New().String() // Non-existent key
	valA := uuid.New().String()
	valB := uuid.New().String()
	valX := uuid.New().String()

	bag["mset_keyA"] = keyA
	bag["mset_keyB"] = keyB
	bag["mset_valB"] = valB

	// MSET keyA valA keyB valB keyX valX
	msetCmd := fmt.Sprintf("MSET %s %s %s %s %s %s", keyA, valA, keyB, valB, keyX, valX)
	if _, err := do(ctx, program, msetCmd); err != nil {
		return rubricItem(fmt.Sprintf("MSET failed: %v", err), 0)
	}

	// MGET keyB keyA keyZ -> expect valB, valA, nil
	mgetCmd := fmt.Sprintf("MGET %s %s %s", keyB, keyA, keyZ)
	out, err := do(ctx, program, mgetCmd)
	if err != nil {
		return rubricItem(fmt.Sprintf("MGET failed: %v", err), 0)
	}

	if len(out) < 3 {
		return rubricItem(fmt.Sprintf("MGET returned too few lines, expected 3, got %d", len(out)), 0)
	}

	// Check keyB -> valB
	if !strings.Contains(strings.TrimSpace(out[0]), valB) {
		return rubricItem(fmt.Sprintf("MGET %s returned wrong value, expected '%s', got '%s'", keyB, valB, out[0]), 0)
	}

	// Check keyA -> valA
	if !strings.Contains(strings.TrimSpace(out[1]), valA) {
		return rubricItem(fmt.Sprintf("MGET %s returned wrong value, expected '%s', got '%s'", keyA, valA, out[1]), 0)
	}

	// Check keyZ -> "nil" or empty
	trimmed := strings.TrimSpace(out[2])
	if trimmed != "" && !strings.Contains(strings.ToLower(trimmed), "nil") {
		return rubricItem(fmt.Sprintf("MGET %s should return nil, got '%s'", keyZ, trimmed), 0)
	}

	return rubricItem("MSET and MGET work correctly", 5)
}

// EvaluateTTLBasic checks EXPIRE and TTL with lazy expiration
func EvaluateTTLBasic(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "TTLBasic",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	key := uuid.New().String()
	value := uuid.New().String()
	bag["ttl_key"] = key

	// SET key value
	if _, err := do(ctx, program, fmt.Sprintf("SET %s %s", key, value)); err != nil {
		return rubricItem(fmt.Sprintf("SET failed: %v", err), 0)
	}

	// EXPIRE key 100
	out, err := do(ctx, program, fmt.Sprintf("EXPIRE %s 100", key))
	if err != nil {
		return rubricItem(fmt.Sprintf("EXPIRE failed: %v", err), 0)
	}
	if len(out) == 0 || !strings.Contains(strings.TrimSpace(out[0]), "1") {
		return rubricItem(fmt.Sprintf("EXPIRE should return 1, got '%v'", out), 0)
	}

	// GET key -> expect value
	out, err = do(ctx, program, fmt.Sprintf("GET %s", key))
	if err != nil {
		return rubricItem(fmt.Sprintf("GET failed: %v", err), 0)
	}
	if len(out) == 0 || !strings.Contains(strings.TrimSpace(out[0]), value) {
		return rubricItem(fmt.Sprintf("GET returned wrong value, expected '%s', got '%v'", value, out), 0)
	}

	// Sleep to let key expire (EXPIRE sets 100ms, add buffer to ensure expiration)
	time.Sleep(expiryCheckDelay)

	// GET key -> expect "nil" or empty
	out, err = do(ctx, program, fmt.Sprintf("GET %s", key))
	if err != nil {
		return rubricItem(fmt.Sprintf("GET after expiry failed: %v", err), 0)
	}
	if len(out) > 0 {
		trimmed := strings.TrimSpace(out[0])
		if trimmed != "" && !strings.Contains(strings.ToLower(trimmed), "nil") {
			return rubricItem(fmt.Sprintf("GET after expiry should return nil, got '%s'", trimmed), 0)
		}
	}

	// TTL key -> expect "-2" (key doesn't exist)
	out, err = do(ctx, program, fmt.Sprintf("TTL %s", key))
	if err != nil {
		return rubricItem(fmt.Sprintf("TTL failed: %v", err), 0)
	}
	if len(out) == 0 || !strings.Contains(strings.TrimSpace(out[0]), "-2") {
		return rubricItem(fmt.Sprintf("TTL should return -2 for expired key, got '%v'", out), 0)
	}

	return rubricItem("EXPIRE and TTL work correctly with lazy expiration", 5)
}

// EvaluateRange checks RANGE command with lexicographic ordering
func EvaluateRange(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "Range",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	// Use deterministic keys for RANGE testing (lexicographic order matters)
	// Generate unique values but use predictable keys
	keys := []string{"a", "b", "c", "d", "e"}
	values := make([]string, len(keys))
	for i := range values {
		values[i] = uuid.New().String()
	}
	bag["range_keys"] = keys

	// MSET a val1 b val2 c val3 d val4 e val5
	msetCmd := fmt.Sprintf("MSET %s %s %s %s %s %s %s %s %s %s",
		keys[0], values[0], keys[1], values[1], keys[2], values[2],
		keys[3], values[3], keys[4], values[4])
	if _, err := do(ctx, program, msetCmd); err != nil {
		return rubricItem(fmt.Sprintf("MSET failed: %v", err), 0)
	}

	// RANGE b d -> expect "b", "c", "d", "END"
	out, err := do(ctx, program, "RANGE b d")
	if err != nil {
		return rubricItem(fmt.Sprintf("RANGE b d failed: %v", err), 0)
	}
	expected := []string{"b", "c", "d", "END"}
	if !validateRangeOutput(out, expected) {
		return rubricItem(fmt.Sprintf("RANGE b d returned wrong keys, expected %v, got %v", expected, out), 0)
	}

	// RANGE "" c -> expect "a", "b", "c", "END"
	out, err = do(ctx, program, `RANGE "" c`)
	if err != nil {
		return rubricItem(fmt.Sprintf("RANGE \"\" c failed: %v", err), 0)
	}
	expected = []string{"a", "b", "c", "END"}
	if !validateRangeOutput(out, expected) {
		return rubricItem(fmt.Sprintf("RANGE \"\" c returned wrong keys, expected %v, got %v", expected, out), 0)
	}

	// RANGE d "" -> expect "d", "e", "END"
	out, err = do(ctx, program, `RANGE d ""`)
	if err != nil {
		return rubricItem(fmt.Sprintf("RANGE d \"\" failed: %v", err), 0)
	}
	expected = []string{"d", "e", "END"}
	if !validateRangeOutput(out, expected) {
		return rubricItem(fmt.Sprintf("RANGE d \"\" returned wrong keys, expected %v, got %v", expected, out), 0)
	}

	return rubricItem("RANGE works correctly with lexicographic ordering", 5)
}

// validateRangeOutput checks if the output matches expected keys (flexible with whitespace/prompts)
func validateRangeOutput(output, expected []string) bool {
	filtered := make([]string, 0, len(output))
	for _, line := range output {
		trimmed := strings.TrimSpace(line)
		// Remove prompt characters
		trimmed = strings.TrimLeftFunc(trimmed, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}

	if len(filtered) != len(expected) {
		return false
	}

	for i, exp := range expected {
		if !strings.EqualFold(filtered[i], exp) {
			return false
		}
	}
	return true
}

// EvaluateTransactions checks BEGIN/COMMIT/ABORT with persistence
func EvaluateTransactions(ctx context.Context, program ProgramRunner, bag RunBag) RubricItem {
	rubricItem := func(msg string, awarded float64) RubricItem {
		return RubricItem{
			Name:    "Transactions",
			Note:    msg,
			Awarded: awarded,
			Points:  5,
		}
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Execution failed: %v", err), 0)
	}

	keyAbort := uuid.New().String()
	valAbort := uuid.New().String()
	keyCommit := uuid.New().String()
	valCommit := uuid.New().String()

	bag["txn_keyAbort"] = keyAbort
	bag["txn_keyCommit"] = keyCommit

	// BEGIN
	if _, err := do(ctx, program, "BEGIN"); err != nil {
		return rubricItem(fmt.Sprintf("BEGIN failed: %v", err), 0)
	}

	// SET keyAbort valAbort
	if _, err := do(ctx, program, fmt.Sprintf("SET %s %s", keyAbort, valAbort)); err != nil {
		return rubricItem(fmt.Sprintf("SET in transaction failed: %v", err), 0)
	}

	// GET keyAbort -> expect valAbort (read-your-writes)
	out, err := do(ctx, program, fmt.Sprintf("GET %s", keyAbort))
	if err != nil {
		return rubricItem(fmt.Sprintf("GET in transaction failed: %v", err), 0)
	}
	if len(out) == 0 || !strings.Contains(strings.TrimSpace(out[0]), valAbort) {
		return rubricItem(fmt.Sprintf("GET in transaction should return '%s', got '%v'", valAbort, out), 0)
	}

	// ABORT
	if _, err := do(ctx, program, "ABORT"); err != nil {
		return rubricItem(fmt.Sprintf("ABORT failed: %v", err), 0)
	}

	// GET keyAbort -> expect "nil" (abort discards)
	out, err = do(ctx, program, fmt.Sprintf("GET %s", keyAbort))
	if err != nil {
		return rubricItem(fmt.Sprintf("GET after ABORT failed: %v", err), 0)
	}
	if len(out) > 0 {
		trimmed := strings.TrimSpace(out[0])
		if trimmed != "" && !strings.Contains(strings.ToLower(trimmed), "nil") {
			return rubricItem(fmt.Sprintf("GET after ABORT should return nil, got '%s'", trimmed), 0)
		}
	}

	// BEGIN
	if _, err := do(ctx, program, "BEGIN"); err != nil {
		return rubricItem(fmt.Sprintf("Second BEGIN failed: %v", err), 0)
	}

	// SET keyCommit valCommit
	if _, err := do(ctx, program, fmt.Sprintf("SET %s %s", keyCommit, valCommit)); err != nil {
		return rubricItem(fmt.Sprintf("SET in second transaction failed: %v", err), 0)
	}

	// COMMIT
	if _, err := do(ctx, program, "COMMIT"); err != nil {
		return rubricItem(fmt.Sprintf("COMMIT failed: %v", err), 0)
	}

	// Kill and restart to check persistence
	if err := program.Kill(); err != nil {
		return rubricItem(fmt.Sprintf("Kill failed: %v", err), 0)
	}
	if err := program.Run(); err != nil {
		return rubricItem(fmt.Sprintf("Restart failed: %v", err), 0)
	}
	time.Sleep(restartLoadDelay) // Wait for program to load persisted data

	// GET keyCommit -> expect valCommit (commit persists)
	out, err = do(ctx, program, fmt.Sprintf("GET %s", keyCommit))
	if err != nil {
		return rubricItem(fmt.Sprintf("GET after restart failed: %v", err), 0)
	}
	if len(out) == 0 || !strings.Contains(strings.TrimSpace(out[0]), valCommit) {
		return rubricItem(fmt.Sprintf("GET after restart should return '%s', got '%v'", valCommit, out), 0)
	}

	return rubricItem("Transactions work correctly with read-your-writes, abort, and commit persistence", 5)
}
