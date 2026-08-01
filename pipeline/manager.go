// Package pipeline orchestrates a single source-to-sink restream pipeline,
// managing source discovery, FFmpeg process lifecycle, and retry/backoff.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"time"

	"restream/config"
	"restream/ffmpeg"
	"restream/health"
	"restream/sink"
	"restream/source"
)

// Manager coordinates the lifecycle of one restream pipeline.
type Manager struct {
	name   string
	cfg    config.Pipeline
	source source.Source
	sink   sink.Sink
}

// NewManager creates a Manager from a pipeline configuration.
func NewManager(cfg config.Pipeline) (*Manager, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("pipeline name is required")
	}

	src, err := source.New(cfg.Source.Type, cfg.Source.Config)
	if err != nil {
		return nil, fmt.Errorf("creating source %q: %w", cfg.Source.Type, err)
	}

	snk, err := sink.New(cfg.Sink.Type, cfg.Sink.Config)
	if err != nil {
		return nil, fmt.Errorf("creating sink %q: %w", cfg.Sink.Type, err)
	}

	return &Manager{
		name:   cfg.Name,
		cfg:    cfg,
		source: src,
		sink:   snk,
	}, nil
}

// Name returns the pipeline name.
func (m *Manager) Name() string {
	return m.name
}

// Run executes the pipeline lifecycle: resolve source stream, build FFmpeg
// command, execute it, and retry on failure with exponential backoff.
func (m *Manager) Run(ctx context.Context) error {
	l := slog.With("pipeline", m.name)

	retriesLeft := m.cfg.Retry.MaxRetries
	interval := time.Duration(m.cfg.Retry.InitialInterval) * time.Second
	maxInterval := time.Duration(m.cfg.Retry.MaxInterval) * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := m.runOnce(ctx, l); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			l.Error("pipeline failed", "error", err)

			if m.cfg.Retry.MaxRetries > 0 {
				if retriesLeft <= 0 {
					l.Error("max retries reached, giving up")
					return fmt.Errorf("pipeline %q: max retries reached", m.name)
				}
				retriesLeft--
			}

			l.Info("retrying",
				"retries_left", retriesLeft,
				"interval_seconds", interval.Seconds(),
			)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}

			// Exponential backoff, capped at maxInterval.
			newInterval := time.Duration(float64(interval) * m.cfg.Retry.BackoffMultiplier)
			if newInterval > maxInterval {
				newInterval = maxInterval
			}
			interval = newInterval
		} else {
			// FFmpeg exited cleanly (unlikely for a live stream).
			l.Info("ffmpeg exited cleanly")
			return nil
		}
	}
}

// runOnce resolves the source stream with yt-dlp, builds the FFmpeg
// command, runs it with health monitoring, and returns nil on clean
// exit or an error.
func (m *Manager) runOnce(ctx context.Context, l *slog.Logger) error {
	// Resolve stream URLs (used as FFmpeg inputs) and codec metadata. The
	// codecs drive the auto-transcode decision in BuildCommand.
	l.Debug("resolving source stream", "url", m.cfg.Source.Config["url"])
	streamInfo, err := m.source.GetStream(ctx, m.cfg.Source.Config["url"])
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}
	l.Debug("source resolved",
		"urls", len(streamInfo.URLs),
		"video_codec", streamInfo.VideoCodec,
		"audio_codec", streamInfo.AudioCodec,
	)

	// Get sink target.
	target, err := m.sink.GetTarget(ctx, m.cfg.Sink.Config)
	if err != nil {
		return fmt.Errorf("get sink target: %w", err)
	}

	// Build the FFmpeg command. yt-dlp has already resolved the stream
	// URLs (GetStream); FFmpeg connects to them directly.
	ffCmd := ffmpeg.NewPipeline(ffmpeg.PipelineConfig{
		StreamInfo: streamInfo,
		Target:     target,
		Ffmpeg:     m.cfg.FFmpeg,
	}).BuildCommand(ctx)
	l.Debug("ffmpeg command", "args", strings.Join(ffCmd.Args, " "))

	// Pipe stderr for health monitoring.
	stderrPipe, err := ffCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	l.Info("starting ffmpeg pipeline",
		"transcode", m.cfg.FFmpeg.Transcode,
		"video", m.cfg.FFmpeg.VideoEncoder,
		"audio", m.cfg.FFmpeg.AudioEncoder,
	)

	if err := ffCmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	l.Info("ffmpeg started", "pid", ffCmd.Process.Pid)

	// Start health monitoring.
	healthTimeout := time.Duration(m.cfg.Retry.InitialInterval) * time.Second * 3
	checker := health.NewChecker(healthTimeout)
	healthCh := checker.Start(ctx, stderrPipe)

	// Wait for FFmpeg completion.
	doneCh := make(chan error, 1)
	go func() { doneCh <- ffCmd.Wait() }()

	select {
	case <-ctx.Done():
		l.Info("graceful shutdown, sending SIGTERM to ffmpeg")
		ffCmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-doneCh:
		case <-time.After(5 * time.Second):
			ffCmd.Process.Kill()
			<-doneCh
		}
		return ctx.Err()
	case status := <-healthCh:
		l.Warn("health check failed, killing ffmpeg", "status", status)
		ffCmd.Process.Kill()
		<-doneCh
		return fmt.Errorf("health check: status=%v", status)
	case err := <-doneCh:
		if err != nil {
			return fmt.Errorf("ffmpeg exited: %w", err)
		}
		l.Info("ffmpeg exited cleanly")
		return nil
	}
}
