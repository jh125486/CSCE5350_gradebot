package main

import (
	"context"
	"crypto/sha256"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/jh125486/CSCE5350_gradebot/pkg/app"
)

var buildID string

func main() {
	// Load .env file if it exists
	_ = godotenv.Load()

	if buildID == "" {
		buildID = os.Getenv("BUILD_ID")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.New(ctx, "gradebot", sha256.Sum256([]byte(buildID))).
		Run(ctx); err != nil {
		log.Fatalf("Failed to execute command: %v", err)
	}

	// tiny grace period for logs to flush
	time.Sleep(10 * time.Millisecond)
}
