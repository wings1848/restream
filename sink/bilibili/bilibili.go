// Package bilibili implements the Sink interface for Bilibili live RTMP push.
package bilibili

import (
	"context"
	"fmt"

	"restream/sink"
)

func init() {
	sink.Register("bilibili", New)
}

// DefaultRTMPURL is the standard Bilibili live push ingest server.
// Users may override it via the rtmp_url config key.
const DefaultRTMPURL = "rtmp://live-push.bilivideo.com/live-bvc/"

// Bilibili implements sink.Sink for Bilibili RTMP push.
type Bilibili struct{}

// New is the Factory registered under the name "bilibili".
func New(config map[string]string) (sink.Sink, error) {
	return &Bilibili{}, nil
}

func (b *Bilibili) Name() string { return "bilibili" }

func (b *Bilibili) ValidateConfig(config map[string]string) error {
	key, ok := config["stream_key"]
	if !ok || key == "" {
		return fmt.Errorf("bilibili sink requires a non-empty \"stream_key\" in config")
	}
	return nil
}

func (b *Bilibili) GetTarget(ctx context.Context, config map[string]string) (*sink.RTMPTarget, error) {
	if err := b.ValidateConfig(config); err != nil {
		return nil, err
	}

	rtmpURL := config["rtmp_url"]
	if rtmpURL == "" {
		rtmpURL = DefaultRTMPURL
	}

	return &sink.RTMPTarget{
		URL:       rtmpURL,
		StreamKey: config["stream_key"],
	}, nil
}
