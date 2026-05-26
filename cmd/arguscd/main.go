package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ellipselv/argus-cd/internal/agent"
)

func main() {
	configPath := flag.String("config", "/etc/argus/config.toml", "path to argus TOML config")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		slog.Error("load config", "path", *configPath, "err", err)
		os.Exit(1)
	}
	runner, err := agent.NewRunner(cfg)
	if err != nil {
		slog.Error("init runner", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	runner.Run(ctx)
}
