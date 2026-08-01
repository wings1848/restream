// Package direct implements a Source that feeds a pre-resolved stream URL
// straight to FFmpeg, skipping yt-dlp entirely.
//
// Why it exists: YouTube's bot checks hit the resolution step (the player
// page), not the signed manifest URLs it produces. A machine on a
// residential IP can resolve a signed HLS URL that is then fetched from any
// other IP — including datacenter IPs that would fail their own resolution
// with "Sign in to confirm you're not a bot". Split the roles: resolve on a
// trusted machine, stream from the cheap/datacenter box.
package direct

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wings1848/restream/source"
)

func init() {
	source.Register("direct", New)
}

// Direct implements source.Source for a pre-resolved stream URL.
type Direct struct {
	// url is the static stream URL; used when urlFile is empty.
	url string

	// urlFile, when set, holds the stream URL (trailing newline allowed).
	// GetStream re-reads it on every resolve, so an external refresher can
	// rotate the URL in place — the next reconnect picks it up.
	urlFile string
}

// New is the Factory registered under the name "direct".
func New(config map[string]string) (source.Source, error) {
	d := &Direct{}
	if v, ok := config["url"]; ok && v != "" {
		d.url = v
	}
	if v, ok := config["url_file"]; ok && v != "" {
		d.urlFile = v
	}
	if d.url == "" && d.urlFile == "" {
		return nil, fmt.Errorf("direct: provide either url or url_file")
	}
	return d, nil
}

func (d *Direct) Name() string { return "direct" }

func (d *Direct) ValidateURL(u string) error {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("direct: not an http(s) URL: %s", u)
	}
	return nil
}

// GetStream returns the configured stream URL as-is. Video/AudioCodec are
// left empty: BuildCommand's auto mode defaults unknown codecs to stream
// copy, which fits the typical H.264/AAC HLS output. Set
// ffmpeg.transcode=force if the source isn't FLV-compatible.
func (d *Direct) GetStream(ctx context.Context, url string) (*source.StreamInfo, error) {
	streamURL := d.url
	if d.urlFile != "" {
		b, err := os.ReadFile(d.urlFile)
		if err != nil {
			return nil, fmt.Errorf("direct: read url_file %s: %w", d.urlFile, err)
		}
		streamURL = strings.TrimSpace(string(b))
	}
	if streamURL == "" {
		return nil, fmt.Errorf("direct: empty stream URL")
	}
	if err := d.ValidateURL(streamURL); err != nil {
		return nil, err
	}
	return &source.StreamInfo{
		URLs:      []string{streamURL},
		Container: "hls",
	}, nil
}
