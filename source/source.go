// Package source defines the Source interface for live stream inputs.
// Each platform (YouTube, Douyin, Twitch, etc.) implements this interface
// and registers itself via the registry to be discoverable by name.
package source

import (
	"context"
	"os/exec"
)

// StreamInfo holds metadata and access URLs for a detected live stream.
type StreamInfo struct {
	// URLs is the list of stream URLs. For HLS/DASH this is a single URL;
	// for platforms that provide separate video/audio streams, both are listed.
	URLs []string

	// Codec information, populated when ProbeFormat succeeds.
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
	// access URLs and metadata needed by FFmpeg.
	GetStream(ctx context.Context, url string) (*StreamInfo, error)

	// ValidateURL checks whether the provided URL is a valid live-stream
	// URL for this platform. Returns nil if valid, an error otherwise.
	ValidateURL(url string) error

	// ProbeFormat probes the stream to determine its codec and container
	// format. Returns nil if probing is not supported for this source.
	ProbeFormat(ctx context.Context, url string) (*StreamInfo, error)

	// BuildStreamCmd builds a command that downloads the live stream to
	// stdout. This handles all authentication (cookies, proxy, tokens)
	// so FFmpeg can read from stdin without worrying about auth.
	BuildStreamCmd(ctx context.Context, url, format string) *exec.Cmd
}

// Factory is a constructor that takes a flat string→string config map and
// returns a configured Source instance.
type Factory func(config map[string]string) (Source, error)
