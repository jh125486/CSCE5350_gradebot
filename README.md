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

## Installation

### From Releases

Download the latest release from [GitHub Releases](https://github.com/jh125486/CSCE5350_gradebot/releases):

- **Linux**: `gradebot-linux-amd64`
- **macOS Intel**: `gradebot-darwin-amd64`
- **macOS Apple Silicon**: `gradebot-darwin-arm64`
- **Windows**: `gradebot-windows-amd64.exe`

### From Source

```bash
git clone https://github.com/jh125486/CSCE5350_gradebot.git
cd CSCE5350_gradebot
go build -o gradebot .
```

## Usage

Submit assignments for grading:

```bash
# Project 1
./gradebot project1 --dir /path/to/project --run "python main.py"

# Project 2
./gradebot project2 --dir /path/to/project --run "go run main.go"
```