package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"contractpro/document-processing/internal/app"
	"contractpro/document-processing/internal/config"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v", err)
		return 1
	}

	application, err := app.New(ctx, cfg)
	if err != nil {
		log.Printf("app init: %v", err)
		return 1
	}

	return application.Run(ctx)
}
