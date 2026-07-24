// Package sink defines the Sink interface for live-stream push destinations.
// Each platform (Bilibili, Douyin, Huya, etc.) implements this interface
// and registers itself via the registry to be discoverable by name.
package sink

import "context"

// RTMPTarget holds the connection details for RTMP push.
type RTMPTarget struct {
	// URL is the RTMP server address (e.g. "rtmp://live-push.bilivideo.com/live-bvc/").
	URL string

	// StreamKey is the authentication component appended to the URL
	// (e.g. "?streamname=live_xxx&key=yyy").
	StreamKey string
}

// FullURL returns the complete RTMP URL by concatenating URL and StreamKey.
func (t *RTMPTarget) FullURL() string {
	return t.URL + t.StreamKey
}

// Sink is the interface every push-destination platform must implement.
type Sink interface {
	// Name returns the sink platform identifier (e.g. "bilibili", "douyin").
	Name() string

	// GetTarget resolves configuration into a ready-to-use RTMP target.
	GetTarget(ctx context.Context, config map[string]string) (*RTMPTarget, error)

	// ValidateConfig checks that the provided configuration contains all
	// required keys and valid values. Returns nil if valid.
	ValidateConfig(config map[string]string) error
}

// Factory is a constructor that takes a flat string→string config map and
// returns a configured Sink instance.
type Factory func(config map[string]string) (Sink, error)
