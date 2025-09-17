# CSCE 5350 Gradebot

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
- **Database**: SQLite for storing submissions and results

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

3. **Install dependencies and build**:
   ```bash
   go mod tidy
   make build
   ```

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

## Usage

Submit assignments for grading:

```bash
# Project 1
./gradebot project1 --dir /path/to/project --run "python main.py"

# Project 2
./gradebot project2 --dir /path/to/project --run "go run main.go"
```

## Local Development with Dev Container

The gradebot includes a complete dev container setup with LocalStack for seamless development and testing.

### Prerequisites

- **VS Code** with the [Dev Containers extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers)
- **Docker** and **Docker Compose**

### Getting Started

1. **Open in Dev Container**:
   - Open the project in VS Code
   - When prompted, click "Reopen in Container" or use Command Palette: `Dev Containers: Reopen in Container`
   - The dev container will automatically start LocalStack and set up the development environment

2. **Verify Setup**:
   ```bash
   # Check LocalStack status
   curl http://localhost:4566/_localstack/health

   # List available S3 buckets
   awslocal s3 ls
   ```

3. **Run Tests**:
   ```bash
   # Run storage tests
   go test ./pkg/storage -v

   # Run all tests
   go test ./... -v
   ```

### What's Included

- **Go 1.24** with all necessary tools
- **LocalStack** for S3-compatible storage testing
- **Pre-configured VS Code extensions** (Go, Docker, GitHub Copilot, etc.)
- **Automatic bucket creation** and environment setup
- **Port forwarding** for both the app (8080) and LocalStack (4566)

### Manual LocalStack Setup (Alternative)

If you prefer not to use the dev container:

```bash
# Start LocalStack manually
docker run -d -p 4566:4566 \
  -e SERVICES=s3 \
  -e DEBUG=1 \
  -e S3_SKIP_SIGNATURE_VALIDATION=1 \
  --name localstack localstack/localstack:latest

# Set environment variables
export R2_ENDPOINT=http://localhost:4566
export R2_BUCKET=test-gradebot-bucket
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export USE_PATH_STYLE=true

# Create bucket
docker exec localstack awslocal s3 mb s3://test-gradebot-bucket

# Run tests
go test ./pkg/storage -v
```

### AWS CLI Setup

For LocalStack interaction, you have several options:

1. **VS Code Extension**: Install the "AWS Toolkit" extension for VS Code
2. **Local Installation**: Install AWS CLI v2 on your host machine
3. **Web UI**: Use LocalStack's web interface at http://localhost:4566
4. **Docker**: Run AWS CLI commands via `docker exec` in the LocalStack container

Example using LocalStack web UI:
```bash
# Open in browser
open http://localhost:4566

# Or use curl for API testing
curl http://localhost:4566/_localstack/health
```

### Environment Variables

The dev container automatically sets:

```bash
R2_ENDPOINT=http://localstack:4566
R2_BUCKET=test-gradebot-bucket
AWS_ACCESS_KEY_ID=test
AWS_SECRET_ACCESS_KEY=test
AWS_REGION=us-east-1
USE_PATH_STYLE=true
```

For production deployment:

```bash
R2_ENDPOINT=https://your-account.r2.cloudflarestorage.com
AWS_REGION=auto
R2_BUCKET=your-production-bucket
AWS_ACCESS_KEY_ID=your-r2-access-key
AWS_SECRET_ACCESS_KEY=your-r2-secret-key
# USE_PATH_STYLE is not set (defaults to false for virtual-hosted addressing)
```

**Note**: The `USE_PATH_STYLE=true` environment variable controls whether to use path-style addressing (for LocalStack) or virtual-hosted addressing (for production R2). It accepts standard boolean values: `true`, `false`, `1`, `0`, `t`, `f`, etc. When set to `true`, it also forces the region to `us-east-1` for LocalStack compatibility.

### Cloudflare R2 Setup (Production)

1. **Create R2 bucket** in Cloudflare dashboard
2. **Generate API tokens** with S3-compatible permissions
3. **Set environment variables** as shown above
4. **Deploy to Koyeb** with the environment variables

### Storage Architecture

The storage system uses:
- **Interface-based design** for easy testing and swapping implementations
- **S3-compatible API** works with both LocalStack and Cloudflare R2
- **JSON serialization** for rubric results
- **Automatic bucket creation** (in development)
- **Path-based organization** (`submissions/{id}.json`)

### Benefits of This Approach

- **🆓 Free**: 10GB free on R2, free LocalStack for development
- **🔄 Seamless**: Same code works locally and in production
- **🧪 Testable**: Full test coverage with LocalStack
- **☁️ Cloud-ready**: Works perfectly with Koyeb free tier
- **📈 Scalable**: No storage limits, easy to upgrade
- **🎯 Smart Addressing**: Automatic detection and addressing based on endpoint URL
- **⚙️ Simple Config**: Single `R2_ENDPOINT` variable for both dev and prod