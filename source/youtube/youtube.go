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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wings1848/restream/source"
)

// youtubeURLRe matches valid YouTube live-stream URLs.
var youtubeURLRe = regexp.MustCompile(`^https?://(www\.)?(youtube\.com/(watch\?v=|live/|@[^/]+/live)|youtu\.be/)`)

// cacheTTL bounds how long a resolved StreamInfo is reused on reconnects.
// The HLS URL carries a long expire (~6h), so re-running the full yt-dlp
// extraction (seconds + PO-token solve) on every transient failure is wasted;
// a 10-minute TTL avoids serving an expiring URL.
const cacheTTL = 10 * time.Minute

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

	// cookies is a path to a Netscape-format cookies.txt passed to yt-dlp via
	// --cookies. Required for streams behind YouTube's "Sign in to confirm
	// you're not a bot" check or members-only content.
	cookies string

	// baseArgs holds the fixed yt-dlp arguments, precomputed once in New.
	baseArgs []string

	// mu guards cached/cachedAt.
	mu sync.Mutex
	// cached is the last successful resolution, reused for rapid reconnects.
	cached   *source.StreamInfo
	cachedAt time.Time
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
	if v, ok := config["force_ipv4"]; ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			switch strings.ToLower(v) {
			case "on", "yes":
				b = true
			case "off", "no":
				b = false
			default:
				return nil, fmt.Errorf("youtube: invalid force_ipv4 value %q", v)
			}
		}
		y.forceIPv4 = b
	}
	if v, ok := config["cookies"]; ok && v != "" {
		y.cookies = v
	}

	// Precompute the fixed yt-dlp args once; GetStream only appends "-j" and the URL.
	base := []string{
		"--flat-playlist",
		"--no-warnings",
		"--socket-timeout", "10",
		"-f", y.format,
	}
	if y.proxy != "" {
		base = append(base, "--proxy", y.proxy)
	}
	if y.cookies != "" {
		base = append(base, "--cookies", y.cookies)
	}
	if y.forceIPv4 {
		base = append(base, "--force-ipv4")
	}
	y.baseArgs = base
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

	// Reuse the last successful resolution within the TTL so a rapid
	// reconnect (e.g. a transient ffmpeg failure) skips the full yt-dlp
	// extraction. Return a copy so callers can't mutate the cache.
	y.mu.Lock()
	if y.cached != nil && time.Since(y.cachedAt) < cacheTTL {
		info := *y.cached
		info.URLs = append([]string(nil), y.cached.URLs...)
		y.mu.Unlock()
		return &info, nil
	}
	y.mu.Unlock()

	args := make([]string, 0, len(y.baseArgs)+2)
	args = append(args, y.baseArgs...)
	args = append(args, "-j", url)
	out, err := exec.CommandContext(ctx, "yt-dlp", args...).Output()
	if err != nil && !y.forceIPv4 {
		// Auto IPv4 fallback: yt-dlp's default (stdlib urllib) network stack
		// resolves addresses in order and can hang or fail on a broken IPv6
		// path. One cheap retry forcing IPv4 covers the common "v6 broken, v4
		// fine" case. Skipped when the user already forces IPv4.
		retry := make([]string, 0, len(y.baseArgs)+3)
		retry = append(retry, y.baseArgs...)
		retry = append(retry, "--force-ipv4", "-j", url)
		if out2, err2 := exec.CommandContext(ctx, "yt-dlp", retry...).Output(); err2 == nil {
			out, err = out2, nil
		} else {
			err = fmt.Errorf("%w; ipv4 fallback also failed: %v", err, err2)
		}
	}
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

	info := &source.StreamInfo{
		URLs:       urls,
		VideoCodec: normalizeCodec(raw.Vcodec),
		AudioCodec: normalizeCodec(raw.Acodec),
		Container:  raw.Ext,
		Resolution: raw.Resolution,
		FPS:        raw.FPS,
		Bitrate:    int64(raw.TBR * 1000), // kbps → bps
	}
	y.mu.Lock()
	y.cached = info
	y.cachedAt = time.Now()
	y.mu.Unlock()
	return info, nil
}

// ytExecError extracts stderr from exec.ExitError for better error messages.
// When err wraps the ExitError with extra context (e.g. an IPv4-fallback retry
// note), the outer context is kept instead of being dropped by errors.As.
func ytExecError(cmdName string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if err != exitErr {
			return fmt.Errorf("%s: %v (stderr: %s)", cmdName, err, string(exitErr.Stderr))
		}
		return fmt.Errorf("%s: %s", cmdName, string(exitErr.Stderr))
	}
	return fmt.Errorf("%s: %w", cmdName, err)
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
