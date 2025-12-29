# GitHub Copilot Instructions

## Project Overview

This is a Go-based grading bot for automating code evaluation and rubric-based assessment. It provides automated feedback on student submissions using AI-powered analysis and quality checks.

All code must follow idiomatic Go conventions and standard library best practices.

## Code Style & Conventions

- **Formatting**: Use `gofmt` and `goimports` for all code. Format on save.
- **Functions**: Keep functions small, composable, and testable. Single responsibility principle.
- **Naming**: Use clear, meaningful names. Follow Go conventions (`camelCase` for unexported, `PascalCase` for exported).
- **Interfaces**: Avoid unnecessary abstractions. Define interfaces at point of use, not point of implementation.
- **Dependencies**: Prefer standard library solutions. Only add external dependencies when necessary.
- **Package Organization**: Follow standard Go layout:
  - `cmd/` - Application entrypoints
  - `pkg/` - Public library code
  - `internal/` - Private application code
  - `proto/` - Protocol buffer definitions

## Testing Guidelines

### Required: All tests must be table-driven

- One table-driven test per exportedfunction, covering all edge cases.

✅ **DO:**
```go
func TestFoo(t *testing.T) {
    type args struct {
        input string
    }
    tests := []struct {
        name    string
        args    args
        want    string
        wantErr bool
    }{
        {name: "valid input", args: args{input: "foo"}, want: "bar", wantErr: false},
        {name: "empty input", args: args{input: ""}, want: "", wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Foo(tt.args.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Foo() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("Foo() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

❌ **DON'T:**
```go
func TestFooValid(t *testing.T) { ... }
func TestFooEmpty(t *testing.T) { ... }
func TestFooInvalid(t *testing.T) { ... }
```

### Black-box Testing

- **Use `package_test` for tests** (e.g., `package storage_test`, not `package storage`)
- Import your own package and test only exported functions
- Tests should validate behavior, not implementation details
- Do not test unexported functions directly

### Test Structure

Every table test should include:
- `name` - descriptive test case name
- `args` - optional input fields (specific to function being tested)
- `want` - expected output
- `wantErr` - whether an error is expected (optional, for functions that return errors)
- `setup` - optional setup function for complex test cases
- `verify` - optional verification function for post-test assertions

### Assertions

Use `testify/assert` and `testify/require`:
- `require.*` - Test fails immediately if assertion fails
- `assert.*` - Test continues, accumulating failures

## Copilot Behavior

When generating code:
1. **Suggest idiomatic Go patterns** - Use Go conventions, not patterns from other languages
2. **Follow existing patterns** - Match the style and structure of surrounding code
3. **Auto-generate table tests** - When creating a new function, suggest a table-driven test
4. **Minimize comments** - Only comment non-obvious logic. Code should be self-documenting
5. **Prefer simplicity** - Choose clear, simple solutions over clever ones

## Documentation

### GoDoc Comments

All exported identifiers must have documentation comments:

```go
// NewClient creates a new API client with the given configuration.
// It returns an error if the configuration is invalid.
func NewClient(cfg *Config) (*Client, error) {
    ...
}
```

**Rules:**
- Start with the identifier name
- Use complete sentences
- Explain what, not how
- Document parameters and return values when not obvious
- Avoid redundant comments (e.g., `// GetName gets the name`)

## Security & Reliability

- **No hardcoded secrets** - Use environment variables or configuration
- **Validate inputs** - Check bounds, null values, and malformed data
- **Handle errors** - Never ignore errors. Return or log them appropriately
- **Concurrency safety** - Protect shared state with mutexes. Prefer channels for communication
- **Context propagation** - Pass `context.Context` for cancellation and timeouts
- **Resource cleanup** - Use `defer` for cleanup (close files, connections, etc.)

## Error Handling

```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}

// Check error types
var pathErr *fs.PathError
if errors.As(err, &pathErr) {
    // Handle path-specific error
}
```

## Examples

### Complete Table Test Template

```go
package storage_test

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/myorg/myrepo/pkg/storage"
)

func TestSaveResult(t *testing.T) {
    type args struct {
        id   string
        data []byte
    }
    tests := []struct {
        name    string
        args    args
        wantErr bool
        setup     func(t *testing.T)
        verify    func(t *testing.T)
    }{
        {
            name:    "valid save",
            args:    args{id: "test-001", data: []byte("test data")},
            wantErr: false,
        },
        {
            name:    "empty id",
            args:    args{id: "", data: []byte("data")},
            wantErr: true,
        },
        {
            name:    "nil data",
            args:    args{id: "test-002", data: nil},
            wantErr: false,
        },
        {
            name: "overwrite existing",
            args: args{id: "test-003", data: []byte("new data")},
            setup: func(t *testing.T) {
                // Pre-populate with old data
                err := storage.SaveResult("test-003", []byte("old data"))
                require.NoError(t, err)
            },
            verify: func(t *testing.T) {
                // Verify new data overwrote old
                data, err := storage.LoadResult("test-003")
                require.NoError(t, err)
                assert.Equal(t, []byte("new data"), data)
            },
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if tt.setup != nil {
                tt.setup(t)
            }

            err := storage.SaveResult(tt.args.id, tt.args.data)

            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }

            if tt.verify != nil {
                tt.verify(t)
            }
        })
    }
}
```

### Subtests for Different Scenarios

When testing multiple scenarios within one function, use subtests:

```go
func TestListResultsPaginated(t *testing.T) {
    // Main table-driven test cases
    tests := []struct {
        name     string
        page     int
        pageSize int
        wantCount int
    }{
        {"first page", 1, 10, 10},
        {"last page", 3, 10, 5},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            results, err := ListResultsPaginated(tt.page, tt.pageSize)
            require.NoError(t, err)
            assert.Len(t, results, tt.wantCount)
        })
    }

    // Separate subtest for edge case requiring different setup
    t.Run("empty_storage", func(t *testing.T) {
        emptyStore := createEmptyStorage(t)
        results, err := emptyStore.ListResultsPaginated(1, 10)
        assert.NoError(t, err)
        assert.Empty(t, results)
    })
}
```

## Project-Specific Guidelines

- **Protobuf**: Use protojson for marshaling/unmarshaling proto messages
- **Storage**: All storage operations should accept `context.Context` for cancellation
- **Logging**: Use structured logging with `slog` package
- **HTTP Handlers**: Return errors, don't write directly to response. Let middleware handle error responses
- **AI Integration**: Respect rate limits and handle API errors gracefully

## Common Patterns in This Codebase

### Configuration
```go
// Use constructor pattern with required params and defaults
func NewConfig(endpoint, bucket string, opts ...func(*Config)) *Config {
    cfg := &Config{
        Endpoint: endpoint,
        Bucket:   bucket,
    }
    if cfg.Bucket == "" {
        cfg.Bucket = "default-bucket"
    }
    return cfg
}
```

### Error Wrapping
```go
if err != nil {
    return fmt.Errorf("failed to save result %s: %w", submissionID, err)
}
```

### Testing Helpers
```go
// Test helpers should use t.Helper() and require
func createTestStorage(t *testing.T) *Storage {
    t.Helper()
    s, err := NewStorage(testConfig)
    require.NoError(t, err)
    return s
}
```

---

**Remember**: Write code for humans first, computers second. Clarity beats cleverness.
