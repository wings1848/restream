// Package youtube implements the Source interface for YouTube live streams.
// It shells out to yt-dlp (must be installed on the host) to resolve stream
// URLs and codec metadata.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"restream/source"
)

// youtubeURLRe matches valid YouTube live-stream URLs.
var youtubeURLRe = regexp.MustCompile(`^https?://(www\.)?(youtube\.com/(watch\?v=|live/)|youtu\.be/)`)

func init() {
	source.Register("youtube", New)
}

// YouTube implements source.Source for YouTube live streams.
type YouTube struct {
	// Format controls the yt-dlp format selector (e.g. "best", "bestvideo+bestaudio").
	// Defaults to "best" when empty.
	format string

	// Proxy is an optional HTTP/SOCKS proxy for yt-dlp (passed via --proxy).
	// e.g. "socks5://127.0.0.1:1080" or "http://proxy:8080".
	proxy string

	// forceIPv4 forces yt-dlp to use IPv4. Useful when proxy only supports
	// IPv4 but YouTube CDN signatures default to IPv6.
	forceIPv4 bool
}

// New is the Factory registered under the name "youtube".
func New(config map[string]string) (source.Source, error) {
	y := &YouTube{}
	if v, ok := config["format"]; ok && v != "" {
		y.format = v
	} else {
		y.format = "best"
	}
	if v, ok := config["proxy"]; ok {
		y.proxy = v
	}
	if v, ok := config["force_ipv4"]; ok && v != "" && v != "false" && v != "0" {
		y.forceIPv4 = true
	}
	return y, nil
}

func (y *YouTube) Name() string { return "youtube" }

func (y *YouTube) ValidateURL(url string) error {
	if !youtubeURLRe.MatchString(url) {
		return fmt.Errorf("not a valid YouTube URL: %s", url)
	}
	return nil
}

// GetStream resolves the stream with a single yt-dlp info-dict call,
// returning both the FFmpeg input URLs and the codec metadata. The stream
// URL lives at the top level for a pre-merged format (e.g. live HLS) or in
// requested_formats when video/audio were selected separately.
func (y *YouTube) GetStream(ctx context.Context, url string) (*source.StreamInfo, error) {
	if err := y.ValidateURL(url); err != nil {
		return nil, err
	}

	args := append(y.buildArgs(), "-f", y.format, "-j", url)
	args = append(args, y.buildNetArgs()...)
	out, err := exec.CommandContext(ctx, "yt-dlp", args...).Output()
	if err != nil {
		return nil, ytExecError("yt-dlp", err)
	}

	var raw struct {
		URL              string  `json:"url"`
		Vcodec           string  `json:"vcodec"`
		Acodec           string  `json:"acodec"`
		Ext              string  `json:"ext"`
		Resolution       string  `json:"resolution"`
		FPS              float64 `json:"fps"`
		TBR              float64 `json:"tbr"`
		RequestedFormats []struct {
			URL string `json:"url"`
		} `json:"requested_formats"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing yt-dlp JSON: %w", err)
	}

	var urls []string
	for _, f := range raw.RequestedFormats {
		if f.URL != "" {
			urls = append(urls, f.URL)
		}
	}
	if len(urls) == 0 {
		if raw.URL == "" {
			return nil, fmt.Errorf("yt-dlp returned no stream URL for %s", url)
		}
		urls = []string{raw.URL}
	}

	return &source.StreamInfo{
		URLs:       urls,
		VideoCodec: normalizeCodec(raw.Vcodec),
		AudioCodec: normalizeCodec(raw.Acodec),
		Container:  raw.Ext,
		Resolution: raw.Resolution,
		FPS:        raw.FPS,
		Bitrate:    int64(raw.TBR * 1000), // kbps → bps
	}, nil
}

// buildArgs returns the base yt-dlp args. Authentication (PO Token) is
// handled externally by the bgutil-ytdlp-pot-provider plugin, not via args.
func (y *YouTube) buildArgs() []string {
	return []string{"--flat-playlist", "--no-warnings"}
}

// buildNetArgs returns extra yt-dlp arguments for the network path
// (proxy, force-ipv4). Authentication (PO Token) is handled by the
// bgutil-ytdlp-pot-provider plugin, which yt-dlp auto-discovers.
func (y *YouTube) buildNetArgs() []string {
	var extraArgs []string
	if y.proxy != "" {
		extraArgs = append(extraArgs, "--proxy", y.proxy)
	}
	if y.forceIPv4 {
		extraArgs = append(extraArgs, "--force-ipv4")
	}
	return extraArgs
}

// ytExecError extracts stderr from exec.ExitError for better error messages.
func ytExecError(context string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s: %s", context, string(exitErr.Stderr))
	}
	return fmt.Errorf("%s: %w", context, err)
}

// normalizeCodec maps yt-dlp's ISO BMFF codec identifiers to canonical names
// so NeedsTranscode can match them: "avc1.4D4028" → "h264", "mp4a.40.2" →
// "aac", "vp09.00.10.08" → "vp9". Unrecognized codecs are lowercased as-is.
func normalizeCodec(c string) string {
	switch {
	case strings.HasPrefix(c, "avc1."), c == "avc1", c == "avc":
		return "h264"
	case strings.HasPrefix(c, "mp4a."), c == "mp4a":
		return "aac"
	case strings.HasPrefix(c, "vp09."), strings.HasPrefix(c, "vp9"):
		return "vp9"
	case strings.HasPrefix(c, "av01."), strings.HasPrefix(c, "av1"):
		return "av1"
	case strings.HasPrefix(c, "hvc1."), strings.HasPrefix(c, "hev1."):
		return "hevc"
	}
	return strings.ToLower(c)
}
