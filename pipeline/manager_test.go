package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wings1848/restream/config"
	"github.com/wings1848/restream/sink"
	"github.com/wings1848/restream/source"
)

// fakeSource returns one real resolve failure, then RetryAfterError pacing
// forever. failCalls counts the real failures.
type fakeSource struct {
	calls     int
	failCalls int
}

func (f *fakeSource) Name() string { return "fake" }
func (f *fakeSource) ValidateURL(string) error {
	return nil
}
func (f *fakeSource) GetStream(ctx context.Context, url string) (*source.StreamInfo, error) {
	f.calls++
	if f.calls == 1 {
		f.failCalls++
		return nil, fmt.Errorf("real resolve failure")
	}
	return nil, &source.RetryAfterError{RetryAfter: 5 * time.Millisecond, Err: fmt.Errorf("pacing")}
}

type fakeSink struct{}

func (f *fakeSink) Name() string { return "fake" }
func (f *fakeSink) GetTarget(ctx context.Context, config map[string]string) (*sink.RTMPTarget, error) {
	return nil, nil
}
func (f *fakeSink) ValidateConfig(config map[string]string) error { return nil }

// TestRunRetryAfterErrorDoesNotConsumeBudget verifies that RetryAfterError
// cycles don't drain retriesLeft: with max_retries=2, one real failure plus
// unlimited pacing ticks must NOT hit "max retries reached". Before the fix,
// the second pacing tick gave up permanently mid-cooldown (~3s in), even
// though the source's own pacing floor said "wait and retry".
func TestRunRetryAfterErrorDoesNotConsumeBudget(t *testing.T) {
	src := &fakeSource{}
	m := &Manager{
		name: "test",
		cfg: config.Pipeline{
			Name:   "test",
			Source: config.SourceConfig{Type: "fake", Config: map[string]string{}},
			Sink:   config.SinkConfig{Type: "fake", Config: map[string]string{}},
			Retry:  config.RetryConfig{MaxRetries: 2, InitialInterval: 1, MaxInterval: 10, BackoffMultiplier: 2.0},
		},
		source: src,
		sink:   &fakeSink{},
	}

	// Timeline: call 1 fails at t=0 (budget 2->1, wait 1s), pacing ticks
	// then wait max(grown interval, 5ms) each. With the fix the loop is
	// still pacing when ctx expires at 4s; without it, retriesLeft hits 0
	// at the t=3 tick and Run returns "max retries reached" before ctx.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	err := m.Run(ctx)

	if err == nil {
		t.Fatal("Run returned nil, want ctx deadline error (fake source never succeeds)")
	}
	if strings.Contains(err.Error(), "max retries reached") {
		t.Fatalf("Run gave up on pacing errors: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context deadline exceeded", err)
	}
	// Pacing ticks must not have been counted as failures: after 1 real
	// failure the loop must still be running (>= 3 calls total).
	if src.calls < 3 {
		t.Fatalf("source called %d times, want >= 3 (pacing must continue past 1 real failure + 1 tick)", src.calls)
	}
	if src.failCalls != 1 {
		t.Fatalf("real failures = %d, want 1 (only real failures drain the budget)", src.failCalls)
	}
}

// okSource always resolves successfully.
type okSource struct {
	calls int
}

func (f *okSource) Name() string { return "fake" }
func (f *okSource) ValidateURL(string) error {
	return nil
}
func (f *okSource) GetStream(ctx context.Context, url string) (*source.StreamInfo, error) {
	f.calls++
	return &source.StreamInfo{URLs: []string{"http://fake/live.m3u8"}, VideoCodec: "h264", AudioCodec: "aac"}, nil
}

type okSink struct{}

func (f *okSink) Name() string { return "fake" }
func (f *okSink) GetTarget(ctx context.Context, config map[string]string) (*sink.RTMPTarget, error) {
	return &sink.RTMPTarget{URL: "rtmp://fake/", StreamKey: "x"}, nil
}
func (f *okSink) ValidateConfig(config map[string]string) error { return nil }

// TestRunCleanExitReconnects verifies that a clean ffmpeg exit (the source
// stream ended: HLS playlist stopped advancing, upstream hiccup) does NOT shut
// the relay down — Run keeps looping and reconnects. Before the fix, clean
// exit returned nil and the whole process exited ("restream stopped"),
// leaving a 7x24 relay dead after any upstream hiccup.
func TestRunCleanExitReconnects(t *testing.T) {
	// Fake ffmpeg that exits 0 immediately, simulating an ended source stream.
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "ffmpeg"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	src := &okSource{}
	m := &Manager{
		name: "test",
		cfg: config.Pipeline{
			Name:   "test",
			Source: config.SourceConfig{Type: "fake", Config: map[string]string{}},
			Sink:   config.SinkConfig{Type: "fake", Config: map[string]string{}},
			Retry:  config.RetryConfig{MaxRetries: 0, InitialInterval: 1, MaxInterval: 10, BackoffMultiplier: 2.0},
		},
		source:             src,
		sink:               &okSink{},
		healthCheckTimeout: 3 * time.Second,
	}

	// Each cycle: resolve -> ffmpeg exits 0 -> wait 1s -> reconnect. With
	// ctx expiring at 2.5s the loop must still be reconnecting (calls >= 2),
	// and Run must NOT return nil (the old "restream stopped" behavior).
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	err := m.Run(ctx)

	if err == nil {
		t.Fatal("Run returned nil on clean exit, want reconnect loop until ctx deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context deadline exceeded", err)
	}
	if src.calls < 2 {
		t.Fatalf("source resolved %d times, want >= 2 (relay must reconnect after clean exit)", src.calls)
	}
}
