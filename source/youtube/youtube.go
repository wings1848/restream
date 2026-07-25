// Package youtube implements the Source interface for YouTube live streams.
// It shells out to yt-dlp (must be installed on the host) to extract stream
// URLs and probe format information.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	// Optional path to a Netscape-format cookies file for authenticated
	// access (e.g. members-only live streams).
	cookiesFile string

	// UserAgent overrides the default yt-dlp user-agent when set.
	userAgent string

	// Format controls the yt-dlp format selector (e.g. "best", "bestvideo+bestaudio").
	// Defaults to "best" when empty.
	format string

	// Proxy is an optional HTTP/SOCKS proxy for yt-dlp (passed via --proxy).
	// e.g. "socks5://127.0.0.1:1080" or "http://proxy:8080".
	proxy string

	// poToken is a Proof of Origin token for YouTube's web client.
	// More durable than cookies — extract once via browser DevTools, lasts weeks.
	// See: https://github.com/yt-dlp/yt-dlp/wiki/Extractors#youtube
	poToken string

	// visitorData is paired with poToken for YouTube authentication.
	visitorData string

	// forceIPv4 forces yt-dlp to use IPv4. Useful when proxy only supports
	// IPv4 but YouTube CDN signatures default to IPv6.
	forceIPv4 bool
}

// New is the Factory registered under the name "youtube".
func New(config map[string]string) (source.Source, error) {
	y := &YouTube{}
	if v, ok := config["cookies_file"]; ok {
		y.cookiesFile = v
	}
	if v, ok := config["user_agent"]; ok {
		y.userAgent = v
	}
	if v, ok := config["format"]; ok && v != "" {
		y.format = v
	} else {
		y.format = "best"
	}
	if v, ok := config["proxy"]; ok {
		y.proxy = v
	}
	if v, ok := config["po_token"]; ok {
		y.poToken = v
	}
	if v, ok := config["visitor_data"]; ok {
		y.visitorData = v
	}
	if v, ok := config["force_ipv4"]; ok && v != "" && v != "false" && v != "0" {
		y.forceIPv4 = true
	}
	return y, nil
}

func (y *YouTube) Name() string { return "youtube" }

// BuildStreamCmd builds a yt-dlp command that downloads the stream to stdout.
// All auth (cookies, proxy, poToken) is handled by yt-dlp.
func (y *YouTube) BuildStreamCmd(ctx context.Context, url, format string) *exec.Cmd {
	if format == "" {
		format = y.format
	}
	extra, _ := y.buildAuthArgs()
	args := append(y.buildArgs(), "-f", format, "-o", "-", url)
	args = append(args, extra...)
	return exec.CommandContext(ctx, "yt-dlp", args...)
}

func (y *YouTube) ValidateURL(url string) error {
	if !youtubeURLRe.MatchString(url) {
		return fmt.Errorf("not a valid YouTube URL: %s", url)
	}
	return nil
}

func (y *YouTube) GetStream(ctx context.Context, url string) (*source.StreamInfo, error) {
	if err := y.ValidateURL(url); err != nil {
		return nil, err
	}

	extra, cleanup := y.buildAuthArgs()
	defer cleanup()

	args := append(y.buildArgs(), "-f", y.format, "-g", url)
	args = append(args, extra...)
	out, err := exec.CommandContext(ctx, "yt-dlp", args...).Output()
	if err != nil {
		return nil, ytExecError("yt-dlp", err)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, fmt.Errorf("yt-dlp returned no stream URL for %s", url)
	}

	return &source.StreamInfo{
		URLs: strings.Split(raw, "\n"),
	}, nil
}

func (y *YouTube) ProbeFormat(ctx context.Context, url string) (*source.StreamInfo, error) {
	if err := y.ValidateURL(url); err != nil {
		return nil, err
	}

	extra, cleanup := y.buildAuthArgs()
	defer cleanup()

	args := append(y.buildArgs(), "-f", y.format, "-j", url)
	args = append(args, extra...)
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, ytExecError("yt-dlp probe", err)
	}

	var raw struct {
		Vcodec    string  `json:"vcodec"`
		Acodec    string  `json:"acodec"`
		Ext       string  `json:"ext"`
		Resolution string `json:"resolution"`
		FPS       float64 `json:"fps"`
		TBR       float64 `json:"tbr"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		// Non-fatal: probing is best-effort.
		return nil, fmt.Errorf("parsing yt-dlp JSON: %w", err)
	}

	return &source.StreamInfo{
		VideoCodec: normalizeCodec(raw.Vcodec),
		AudioCodec: normalizeCodec(raw.Acodec),
		Container:  raw.Ext,
		Resolution: raw.Resolution,
		FPS:        raw.FPS,
		Bitrate:    int64(raw.TBR * 1000), // kbps → bps
	}, nil
}

// buildArgs returns the base yt-dlp args without auth.
func (y *YouTube) buildArgs() []string {
	return []string{"--flat-playlist", "--no-warnings"}
}

// buildAuthArgs returns extra yt-dlp arguments for authentication
// (cookies, proxy, poToken) and a cleanup function for temp files.
func (y *YouTube) buildAuthArgs() ([]string, func()) {
	var extraArgs []string
	var cleanup func() = func() {}

	if y.cookiesFile != "" {
		src, err := os.ReadFile(y.cookiesFile)
		if err == nil {
			f, err := os.CreateTemp("", "yt-dlp-*.txt")
			if err == nil {
				f.Write(src)
				f.Close()
				extraArgs = append(extraArgs, "--cookies", f.Name())
				cleanup = func() { os.Remove(f.Name()) }
			}
		}
	}
	if y.userAgent != "" {
		extraArgs = append(extraArgs, "--user-agent", y.userAgent)
	}
	if y.proxy != "" {
		extraArgs = append(extraArgs, "--proxy", y.proxy)
	}
	if y.forceIPv4 {
		extraArgs = append(extraArgs, "--force-ipv4")
	}
	if y.poToken != "" || y.visitorData != "" {
		parts := []string{"youtube:"}
		if y.poToken != "" {
			parts = append(parts, "po_token=web.gvs+"+y.poToken)
		}
		if y.visitorData != "" {
			parts = append(parts, "visitor_data="+y.visitorData)
		}
		extraArgs = append(extraArgs, "--extractor-args", strings.Join(parts, ";"))
	}
	return extraArgs, cleanup
}

// ytExecError extracts stderr from exec.ExitError for better error messages.
func ytExecError(context string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s: %s", context, string(exitErr.Stderr))
	}
	return fmt.Errorf("%s: %w", context, err)
}

// normalizeCodec strips "avc1." / "mp4a." / "vp09." prefixes that
// yt-dlp sometimes includes for ISO BMFF codec identifiers.
func normalizeCodec(c string) string {
	c = strings.TrimPrefix(c, "avc1.")
	c = strings.TrimPrefix(c, "mp4a.")
	c = strings.TrimPrefix(c, "vp09.")
	return strings.ToLower(c)
}
