// Package pipeline orchestrates a single source-to-sink restream pipeline,
// managing source discovery, FFmpeg process lifecycle, and retry/backoff.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"restream/config"
	"restream/ffmpeg"
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

// runOnce resolves the source stream, builds the FFmpeg pipeline, and runs it.
// Returns nil on clean exit, or an error if any step fails.
func (m *Manager) runOnce(ctx context.Context, l *slog.Logger) error {
	// Resolve source stream.
	l.Debug("resolving source stream", "url", m.cfg.Source.Config["url"])
	streamInfo, err := m.source.GetStream(ctx, m.cfg.Source.Config["url"])
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}

	// Get sink target.
	target, err := m.sink.GetTarget(ctx, m.cfg.Sink.Config)
	if err != nil {
		return fmt.Errorf("get sink target: %w", err)
	}

	// Build the FFmpeg command.
	pipe := ffmpeg.NewPipeline(
		streamInfo,
		target,
		ffmpeg.TranscodeMode(m.cfg.FFmpeg.Transcode),
		m.cfg.FFmpeg.VideoEncoder,
		m.cfg.FFmpeg.Preset,
		m.cfg.FFmpeg.CRF,
		m.cfg.FFmpeg.AudioEncoder,
		m.cfg.FFmpeg.AudioBitrate,
	)

	cmd, err := pipe.BuildCommand(ctx)
	if err != nil {
		return fmt.Errorf("build ffmpeg command: %w", err)
	}

	l.Info("starting ffmpeg pipeline",
		"transcode", m.cfg.FFmpeg.Transcode,
		"video", m.cfg.FFmpeg.VideoEncoder,
		"audio", m.cfg.FFmpeg.AudioEncoder,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg exited: %w", err)
	}

	return nil
}
