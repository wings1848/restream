// Package pipeline orchestrates a single source-to-sink restream pipeline,
// managing source discovery, FFmpeg process lifecycle, and retry/backoff.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
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

// runOnce resolves the source stream, builds a yt-dlp → ffmpeg pipeline,
// runs it with health monitoring, and returns nil on clean exit or an error.
func (m *Manager) runOnce(ctx context.Context, l *slog.Logger) error {
	// Resolve stream URLs for codec probing.
	l.Debug("resolving source stream", "url", m.cfg.Source.Config["url"])
	streamInfo, err := m.source.GetStream(ctx, m.cfg.Source.Config["url"])
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}

	// Probe codec format for auto transcode decision.
	if probeInfo, probeErr := m.source.ProbeFormat(ctx, m.cfg.Source.Config["url"]); probeErr == nil {
		streamInfo.VideoCodec = probeInfo.VideoCodec
		streamInfo.AudioCodec = probeInfo.AudioCodec
	}

	// Get sink target.
	target, err := m.sink.GetTarget(ctx, m.cfg.Sink.Config)
	if err != nil {
		return fmt.Errorf("get sink target: %w", err)
	}

	// Build yt-dlp pipe command: downloads the stream to stdout with all
	// auth (cookies, proxy, poToken) handled by yt-dlp.
	ytdlpCmd := m.source.BuildStreamCmd(ctx,
		m.cfg.Source.Config["url"],
		m.cfg.Source.Config["format"],
	)

	// Build the FFmpeg command reading from yt-dlp's stdout pipe.
	ffPipe := ffmpeg.NewPipeline(ffmpeg.PipelineConfig{
		StreamInfo: streamInfo,
		Target:     target,
		Ffmpeg:     m.cfg.FFmpeg,
	})
	ffCmd, err := ffPipe.BuildPipeCommand(ctx, ytdlpCmd)
	if err != nil {
		return fmt.Errorf("build ffmpeg pipeline: %w", err)
	}

	// Pipe stderr for health monitoring.
	stderrPipe, err := ffCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	l.Info("starting yt-dlp | ffmpeg pipeline",
		"transcode", m.cfg.FFmpeg.Transcode,
		"video", m.cfg.FFmpeg.VideoEncoder,
		"audio", m.cfg.FFmpeg.AudioEncoder,
	)

	// Start yt-dlp first (BuildPipeCmd already starts it).
	if err := ffCmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	l.Info("pipeline started", "ffmpeg_pid", ffCmd.Process.Pid)

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
		ytdlpCmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-doneCh:
		case <-time.After(5 * time.Second):
			ffCmd.Process.Kill()
			ytdlpCmd.Process.Kill()
			<-doneCh
		}
		return ctx.Err()
	case status := <-healthCh:
		l.Warn("health check failed, killing pipeline", "status", status)
		ffCmd.Process.Kill()
		ytdlpCmd.Process.Kill()
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
