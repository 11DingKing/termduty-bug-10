// Command termduty-server runs the terminal & case time-limit monitoring HTTP
// service on port 57615 by default, wiring the sharded reading store, the
// alert orchestrator and the background scheduler.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"termduty/internal/config"
	"termduty/internal/crosscut"
	"termduty/internal/domain"
	"termduty/internal/httpapi"
	"termduty/internal/orchestration"
	"termduty/internal/scheduler"
	"termduty/internal/store"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	addr := flag.String("addr", "", "HTTP listen address override (default :57615)")
	dataDir := flag.String("data", "", "data directory override")
	logLevel := flag.String("log-level", "", "log level override (debug|info|warn|error)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal(err)
	}
	if *addr != "" {
		cfg.HTTPAddr = *addr
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
		cfg.DBPath = *dataDir + "/termduty.db"
		cfg.ShardDir = *dataDir + "/shards"
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}
	if err := cfg.Normalize(); err != nil {
		fatal(err)
	}

	log := crosscut.NewLogger(cfg.LogLevel)
	clock := domain.RealClock{}
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(rootCtx, cfg.DBPath, cfg.ShardDir, clock, log)
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	svc := orchestration.New(st, clock, log)
	sched := scheduler.New(svc, cfg, clock, log)
	if err := sched.Start(rootCtx); err != nil {
		fatal(err)
	}

	server := httpapi.New(svc, sched, cfg, log, st)
	server.MarkReady(true)

	errCh := make(chan error, 1)
	go func() { errCh <- server.Start(rootCtx) }()

	select {
	case <-rootCtx.Done():
		log.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			fatal(err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown failed", "err", err)
	}
	if err := sched.Stop(shutdownCtx); err != nil {
		log.Error("scheduler stop failed", "err", err)
	}
	log.Info("server stopped")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
