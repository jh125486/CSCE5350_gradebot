package main

import (
	"context"
	"crypto/sha256"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jh125486/CSCE5350_gradebot/pkg/app"
	basecli "github.com/jh125486/gradebot/pkg/cli"
)

var buildID string

func main() {
	if buildID == "" {
		buildID = os.Getenv("BUILD_ID")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var cli app.CLI
	if err := basecli.NewKongContext(ctx, "gradebot", sha256.Sum256([]byte(buildID)), &cli, os.Args[1:]).
		Run(ctx); err != nil {
		log.Fatalf("Failed to execute command: %v", err)
	}

	// tiny grace period for logs to flush
	time.Sleep(10 * time.Millisecond)
}
