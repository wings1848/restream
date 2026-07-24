// Package youtube implements the Source interface for YouTube live streams.
// It shells out to yt-dlp (must be installed on the host) to extract stream
// URLs and probe format information.
package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"restream/source"
)

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
	return y, nil
}

func (y *YouTube) Name() string { return "youtube" }

func (y *YouTube) ValidateURL(url string) error {
	// Accept youtube.com / youtu.be live URLs.
	matched, _ := regexp.MatchString(
		`^https?://(www\.)?(youtube\.com/(watch\?v=|live/)|youtu\.be/)`,
		url,
	)
	if !matched {
		return fmt.Errorf("not a valid YouTube URL: %s", url)
	}
	return nil
}

func (y *YouTube) GetStream(ctx context.Context, url string) (*source.StreamInfo, error) {
	if err := y.ValidateURL(url); err != nil {
		return nil, err
	}

	// yt-dlp --flat-playlist -f best -g <url>
	// -g prints the direct stream URL(s).
	args := y.baseArgs()
	args = append(args, "-f", "best", "-g", url)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("yt-dlp failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("running yt-dlp: %w", err)
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

	// yt-dlp -f best -j <url>  → single-line JSON with full metadata.
	args := y.baseArgs()
	args = append(args, "-f", "best", "-j", url)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("yt-dlp probe failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("running yt-dlp probe: %w", err)
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

func (y *YouTube) baseArgs() []string {
	args := []string{"--flat-playlist", "--no-warnings"}
	if y.cookiesFile != "" {
		args = append(args, "--cookies", y.cookiesFile)
	}
	if y.userAgent != "" {
		args = append(args, "--user-agent", y.userAgent)
	}
	return args
}

// normalizeCodec strips "avc1." / "mp4a." / "vp09." prefixes that
// yt-dlp sometimes includes for ISO BMFF codec identifiers.
func normalizeCodec(c string) string {
	c = strings.TrimPrefix(c, "avc1.")
	c = strings.TrimPrefix(c, "mp4a.")
	c = strings.TrimPrefix(c, "vp09.")
	return strings.ToLower(c)
}
