// Package main is the entry point for restream, a YouTube-to-Bilibili live
// stream relay with auto-reconnect and extensible source/sink architecture.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/wings1848/restream/config"
	"github.com/wings1848/restream/health"
	"github.com/wings1848/restream/pipeline"

	// Import source/sink implementations so their init() registers them.
	_ "github.com/wings1848/restream/sink/bilibili"
	_ "github.com/wings1848/restream/source/youtube"
)

// version is the restream release version printed by --version.
const version = "v0.1.0"

func main() {
	// Parse CLI flags.
	flags := &config.CLIFlags{}
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.StringVar(&flags.ConfigPath, "config", "", "Path to config.yaml")
	flag.StringVar(&flags.URL, "url", "", "YouTube live URL (quick-start, no config file)")
	flag.StringVar(&flags.StreamKey, "key", "", "Bilibili stream key (quick-start, no config file)")
	flag.StringVar(&flags.Transcode, "transcode", "", "Transcode mode: auto|copy|force")
	flag.StringVar(&flags.LogLevel, "log-level", "", "Log level: debug|info|warn|error (default: config log_level or info)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("restream %s\n", version)
		os.Exit(0)
	}

	// Setup structured text logger. The CLI flag wins over the config file;
	// the final level is set after config loading below.
	level, err := parseLogLevel(flags.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restream: %v\n", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Load and validate configuration.
	cfg, err := config.LoadWithFlags(flags)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// When --log-level is omitted, honor the config file's global.log_level.
	if flags.LogLevel == "" {
		level, err := parseLogLevel(cfg.Global.LogLevel)
		if err != nil {
			logger.Error("invalid log_level in config", "error", err)
			os.Exit(1)
		}
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
		slog.SetDefault(logger)
	}

	logger.Info("restream starting",
		"pipelines", len(cfg.Pipelines),
		"log_level", cfg.Global.LogLevel,
	)

	// Create a root context that cancels on SIGINT or SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, initiating graceful shutdown", "signal", sig)
		cancel()
	}()

	// Health-check stall timeout comes from the global config (seconds).
	healthCheckTimeout := time.Duration(cfg.Global.HealthCheckInterval) * time.Second

	// Shared health registry: pipelines report their status to it and the
	// /healthz HTTP endpoint serves a snapshot.
	reg := health.NewRegistry()

	// Start the /healthz status endpoint if configured (default :8080).
	if cfg.Global.HttpAddr != "" {
		http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(reg.Snapshot())
		})
		go func() {
			logger.Info("healthz endpoint listening", "addr", cfg.Global.HttpAddr)
			if err := http.ListenAndServe(cfg.Global.HttpAddr, nil); err != nil {
				logger.Error("healthz server failed", "error", err)
			}
		}()
	}

	// Launch all pipelines concurrently.
	var wg sync.WaitGroup
	for _, pipeCfg := range cfg.Pipelines {
		wg.Add(1)
		go func(pc config.Pipeline) {
			defer wg.Done()
			mgr, err := pipeline.NewManager(pc, healthCheckTimeout, reg)
			if err != nil {
				// A single bad pipeline should not take down the others.
				logger.Error("failed to create pipeline", "name", pc.Name, "error", err)
				return
			}
			if err := mgr.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Error("pipeline exited with error", "name", pc.Name, "error", err)
				cancel()
			}
		}(pipeCfg)
	}

	wg.Wait()
	logger.Info("restream stopped")
}

// parseLogLevel converts a string level to slog.Level. An empty string means
// the default level (info); any other unrecognised value is an error.
func parseLogLevel(s string) (slog.Level, error) {
	switch s {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid log level %q (expected debug|info|warn|error)", s)
	}
}
