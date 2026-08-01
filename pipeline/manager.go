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

	"github.com/wings1848/restream/config"
	"github.com/wings1848/restream/ffmpeg"
	"github.com/wings1848/restream/health"
	"github.com/wings1848/restream/sink"
	"github.com/wings1848/restream/source"
)

// Manager coordinates the lifecycle of one restream pipeline.
type Manager struct {
	name               string
	cfg                config.Pipeline
	source             source.Source
	sink               sink.Sink
	healthCheckTimeout time.Duration
	reg                *health.Registry
	startedAt          time.Time
}

// NewManager creates a Manager from a pipeline configuration. The
// healthCheckTimeout is the stall timeout applied to FFmpeg health monitoring;
// it is clamped to a minimum of 3 seconds to stay sane. reg, when non-nil, is
// the shared health registry the pipeline reports its status to (for /healthz).
func NewManager(cfg config.Pipeline, healthCheckTimeout time.Duration, reg *health.Registry) (*Manager, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("pipeline name is required")
	}

	if healthCheckTimeout < 3*time.Second {
		healthCheckTimeout = 3 * time.Second
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
		name:               cfg.Name,
		cfg:                cfg,
		source:             src,
		sink:               snk,
		healthCheckTimeout: healthCheckTimeout,
		reg:                reg,
	}, nil
}

// setHealthState publishes this pipeline's status to the shared registry.
func (m *Manager) setHealthState(state health.PipelineState, lastErr string, tail []string) {
	if m.reg == nil {
		return
	}
	st := health.PipelineStatus{State: state}
	if state == health.StateRunning && !m.startedAt.IsZero() {
		st.Started = m.startedAt
		st.Uptime = time.Since(m.startedAt)
	}
	st.LastError = lastErr
	st.StderrTail = tail
	m.reg.Set(m.name, st)
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
	initialInterval := time.Duration(m.cfg.Retry.InitialInterval) * time.Second
	interval := initialInterval
	maxInterval := time.Duration(m.cfg.Retry.MaxInterval) * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		start := time.Now()
		err := m.runOnce(ctx, l)
		runDuration := time.Since(start)

		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			l.Error("pipeline failed", "error", err, "run_duration", runDuration)

			if m.cfg.Retry.MaxRetries > 0 {
				if retriesLeft <= 0 {
					l.Error("max retries reached, giving up")
					return fmt.Errorf("pipeline %q: max retries reached: %w", m.name, err)
				}
				retriesLeft--
			}

			// Reset the backoff when the pipeline ran for a meaningful amount
			// of time before failing (longer than 2x the initial interval).
			// This prevents a stale, grown interval from delaying a quick
			// reconnect after a long healthy session.
			if runDuration > 2*initialInterval {
				interval = initialInterval
			}

			l.Info("retrying",
				"retries_left", retriesLeft,
				"interval_seconds", interval.Seconds(),
			)

			if err := backoffWait(ctx, interval); err != nil {
				return err
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

// backoffWait blocks until the interval elapses or the context is cancelled.
// It uses time.NewTimer and stops the timer on cancellation to avoid leaking
// the underlying timer resource.
func backoffWait(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// runOnce resolves the source stream with yt-dlp, builds the FFmpeg
// command, runs it with health monitoring, and returns nil on clean
// exit or an error.
func (m *Manager) runOnce(ctx context.Context, l *slog.Logger) error {
	m.setHealthState(health.StateResolving, "", nil)

	// Resolve stream URLs (used as FFmpeg inputs) and codec metadata. The
	// codecs drive the auto-transcode decision in BuildCommand.
	l.Debug("resolving source stream", "url", m.cfg.Source.Config["url"])
	streamInfo, err := m.source.GetStream(ctx, m.cfg.Source.Config["url"])
	if err != nil {
		m.setHealthState(health.StateBackoff, err.Error(), nil)
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
		m.setHealthState(health.StateBackoff, err.Error(), nil)
		return fmt.Errorf("get sink target: %w", err)
	}

	// Build the FFmpeg command. yt-dlp has already resolved the stream
	// URLs (GetStream); FFmpeg connects to them directly.
	ffCmd := ffmpeg.NewPipeline(ffmpeg.PipelineConfig{
		StreamInfo: streamInfo,
		Target:     target,
		Ffmpeg:     m.cfg.FFmpeg,
	}).BuildCommand(ctx)
	// Building the argument string is only worth the cost at debug level.
	if l.Enabled(ctx, slog.LevelDebug) {
		l.Debug("ffmpeg command", "args", strings.Join(ffCmd.Args, " "))
	}

	// Pipe stderr for health monitoring.
	stderrPipe, err := ffCmd.StderrPipe()
	if err != nil {
		m.setHealthState(health.StateBackoff, err.Error(), nil)
		return fmt.Errorf("stderr pipe: %w", err)
	}

	l.Info("starting ffmpeg pipeline",
		"transcode", m.cfg.FFmpeg.Transcode,
		"video", m.cfg.FFmpeg.VideoEncoder,
		"audio", m.cfg.FFmpeg.AudioEncoder,
	)

	if err := ffCmd.Start(); err != nil {
		m.setHealthState(health.StateBackoff, err.Error(), nil)
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	m.startedAt = time.Now()
	m.setHealthState(health.StateRunning, "", nil)
	l.Info("ffmpeg started", "pid", ffCmd.Process.Pid)

	// Start health monitoring with the configured health-check interval.
	checker := health.NewChecker(m.healthCheckTimeout, l)
	healthCh := checker.Start(ctx, stderrPipe)

	// Periodically publish ffmpeg stats to the health registry so /healthz
	// reflects live bitrate/fps. Stops when runOnce returns.
	statsStop := make(chan struct{})
	defer close(statsStop)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-statsStop:
				return
			case <-ticker.C:
				s := checker.LatestStats()
				m.setHealthState(health.StateRunning, "", nil)
				if m.reg != nil {
					m.reg.Set(m.name, health.PipelineStatus{
						State:      health.StateRunning,
						Started:    m.startedAt,
						Uptime:     time.Since(m.startedAt),
						Bitrate:    s.Bitrate,
						FPS:        s.FPS,
						StderrTail: checker.Tail(),
					})
				}
			}
		}
	}()

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
		m.setHealthState(health.StateStopped, "shutdown", nil)
		return ctx.Err()
	case status := <-healthCh:
		// FFmpeg may have exited on its own at the same moment the checker
		// reported a stall (stderr EOF). Give Wait a short window to surface
		// the real exit error instead of a spurious health failure. Only when
		// ffmpeg is still running after the window is the health signal
		// treated as authoritative (genuine stall/error -> kill).
		select {
		case err := <-doneCh:
			if err != nil {
				m.setHealthState(health.StateBackoff, err.Error(), checker.Tail())
				return fmt.Errorf("ffmpeg exited: %w", err)
			}
			l.Info("ffmpeg exited cleanly")
			m.setHealthState(health.StateStopped, "", nil)
			return nil
		case <-time.After(300 * time.Millisecond):
		}
		l.Warn("health check failed, killing ffmpeg", "status", status.String())
		ffCmd.Process.Kill()
		<-doneCh
		err := fmt.Errorf("health check: status=%s", status)
		m.setHealthState(health.StateBackoff, err.Error(), checker.Tail())
		return err
	case err := <-doneCh:
		if err != nil {
			m.setHealthState(health.StateBackoff, err.Error(), checker.Tail())
			return fmt.Errorf("ffmpeg exited: %w", err)
		}
		l.Info("ffmpeg exited cleanly")
		m.setHealthState(health.StateStopped, "", nil)
		return nil
	}
}
