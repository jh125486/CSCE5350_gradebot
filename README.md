# CSCE 5350 Gradebot

[![Go Version](https://img.shields.io/github/go-mod/go-version/jh125486/CSCE5350_gradebot)](https://golang.org/)
[![Build Status](https://github.com/jh125486/CSCE5350_gradebot/workflows/test/badge.svg)](https://github.com/jh125486/CSCE5350_gradebot/actions)
[![Coverage Status](https://codecov.io/gh/jh125486/CSCE5350_gradebot/branch/main/graph/badge.svg)](https://codecov.io/gh/jh125486/CSCE5350_gradebot)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=jh125486_CSCE5350_gradebot&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=jh125486_CSCE5350_gradebot)
[![Go Report Card](https://goreportcard.com/badge/github.com/jh125486/CSCE5350_gradebot)](https://goreportcard.com/report/github.com/jh125486/CSCE5350_gradebot)
[![Release](https://img.shields.io/github/release/jh125486/CSCE5350_gradebot.svg)](https://github.com/jh125486/CSCE5350_gradebot/releases)

Automated code grading system for CSCE 5350 assignments.

## Features

- **Server Mode**: HTTP server for receiving and grading code submissions
- **Client Mode**: CLI tool for submitting assignments for grading
- **OpenAI Integration**: Uses GPT-4o Mini for code analysis and feedback
- **Web Interface**: HTML dashboard for viewing submissions and grades
- **Cross-Platform**: Native binaries for Linux, macOS, and Windows
- **Koyeb Deployment**: Optimized for cloud deployment

## Architecture

### Overview

The gradebot consists of:
- **Server**: HTTP API server handling grading requests
- **Client**: CLI tool for submitting assignments
- **Rubrics**: Evaluation logic and test runners

## Local Development

### Setup

1. **Clone the repository**:
   ```bash
   git clone https://github.com/jh125486/CSCE5350_gradebot.git
   cd CSCE5350_gradebot
   ```

2. **Set up environment variables**:
   ```bash
   # Copy the example environment file
   cp .env.example .env
   
   # Edit .env with your actual secrets
   nano .env  # or your preferred editor
   ```

3. **Initialize development environment**:
   ```bash
   go mod tidy
   make init    # Installs git hooks
   make build
   ```
   The `make init` command installs a pre-push hook that runs tests and linting before allowing a push.

### Testing Locally

The application automatically loads environment variables from a `.env` file when running locally.

**Start the server**:
```bash
# Using Makefile
make local-test

# Or manually
./bin/gradebot server --port 8080
```

**Test the client**:
```bash
# Submit a project for grading
./bin/gradebot project1 --dir /path/to/your/project --run "python main.py"
```

### Environment Variables

The following environment variables are required for full functionality:

- `OPENAI_API_KEY`: Your OpenAI API key
- `BUILD_ID`: Unique build identifier for authentication
- `R2_ENDPOINT`: Cloudflare R2 endpoint URL
- `AWS_ACCESS_KEY_ID`: R2 access key
- `AWS_SECRET_ACCESS_KEY`: R2 secret key

Optional variables:
- `R2_BUCKET`: Custom bucket name (defaults to "gradebot-storage")
- `AWS_REGION`: AWS region (defaults to "auto")
- `USE_PATH_STYLE`: Use path-style S3 URLs (for LocalStack testing)

### Development Workflow

**Run tests**:
```bash
make test          # Run all tests with race detection
make test-verbose  # Run tests with verbose output
```

**Run linting**:
```bash
make lint          # Run golangci-lint and security checks
```

**Git Hooks**:
The pre-push hook automatically runs before each `git push` to ensure:
- All tests pass (including race detection)
- Code passes all linting checks

To bypass the hook (not recommended):
```bash
git push --no-verify
```

## Usage

Submit assignments for grading:

```bash
# Project 1
./gradebot project1 --dir /path/to/project --run "python main.py"

# Project 2
./gradebot project2 --dir /path/to/project --run "go run main.go"
```
