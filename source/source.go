// Package source defines the Source interface for live stream inputs.
// Each platform (YouTube, Douyin, Twitch, etc.) implements this interface
// and registers itself via the registry to be discoverable by name.
package source

import (
	"context"
	"fmt"
	"time"
)

// StreamInfo holds metadata and access URLs for a detected live stream.
type StreamInfo struct {
	// URLs is the list of stream URLs. For HLS/DASH this is a single URL;
	// for platforms that provide separate video/audio streams, both are listed.
	URLs []string

	// Codec information, populated by GetStream from the resolved metadata.
	VideoCodec string // e.g. "h264", "av1", "vp9"
	AudioCodec string // e.g. "aac", "opus", "vorbis"
	Container  string // e.g. "hls", "dash", "rtmp", "m3u8"

	// Stream properties.
	Resolution string  // e.g. "1920x1080"
	FPS        float64 // frames per second
	Bitrate    int64   // total bitrate in bits per second
}

// Source is the interface every stream source platform must implement.
type Source interface {
	// Name returns the source platform identifier (e.g. "youtube", "douyin").
	Name() string

	// GetStream resolves the given URL to a StreamInfo with all stream
	// access URLs and metadata (including codecs) needed by FFmpeg.
	GetStream(ctx context.Context, url string) (*StreamInfo, error)

	// ValidateURL checks whether the provided URL is a valid live-stream
	// URL for this platform. Returns nil if valid, an error otherwise.
	ValidateURL(url string) error
}

// Factory is a constructor that takes a flat string→string config map and
// returns a configured Source instance.
type Factory func(config map[string]string) (Source, error)

// RetryAfterError is returned by GetStream when a resolve must be postponed —
// e.g. a cooldown after a failed extraction. The pipeline manager treats it
// like a resolve failure but waits at least RetryAfter before the next
// attempt: retry pacing stays in the manager's backoff loop, while the source
// still sets its own minimum retry floor.
type RetryAfterError struct {
	// RetryAfter is the minimum delay before the next resolve attempt.
	RetryAfter time.Duration
	// Err is the underlying failure.
	Err error
}

func (e *RetryAfterError) Error() string {
	return fmt.Sprintf("%v (retry after %s)", e.Err, e.RetryAfter)
}

func (e *RetryAfterError) Unwrap() error {
	return e.Err
}
