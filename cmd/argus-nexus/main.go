package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ellipselv/argus-cd/nexus"
)

func main() {
	// 
	configPath := flag.String("config", "/etc/argus/nexus.yaml", "path to nexus config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := nexus.LoadConfig(*configPath)
	if err != nil {
		slog.Error("load config", "path", *configPath, "err", err)
		os.Exit(1)
	}
	runner, err := nexus.NewRunner(cfg)
	if err != nil {
		slog.Error("init runner", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	runner.Run(ctx)
}
