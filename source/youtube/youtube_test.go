package youtube

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wings1848/restream/source"
)

// writeFakeYtDlp installs a fake yt-dlp script with the given body into dir
// and prepends dir to PATH so the yt-dlp invocations in GetStream hit it.
func writeFakeYtDlp(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestGetStreamCacheHit verifies that a second GetStream within the cache TTL
// reuses the first resolution instead of re-running yt-dlp. A fake yt-dlp
// script emits a distinct URL on every invocation and counts calls; the second
// GetStream must return the FIRST URL (cache hit), proving yt-dlp ran once.
func TestGetStreamCacheHit(t *testing.T) {
	dir := t.TempDir()
	countFile := filepath.Join(dir, "count")
	scriptBody := `#!/bin/sh
n=0
[ -f "` + countFile + `" ] && n=$(cat "` + countFile + `")
echo $((n+1)) > "` + countFile + `"
echo "{\"url\":\"http://fake-hls/$n.m3u8\",\"vcodec\":\"h264\",\"acodec\":\"aac\",\"ext\":\"mp4\",\"fps\":30,\"tbr\":4000}"
`
	writeFakeYtDlp(t, dir, scriptBody)

	src, err := New(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const url = "https://www.youtube.com/watch?v=abc123"

	info1, err := src.GetStream(ctx, url)
	if err != nil {
		t.Fatalf("first GetStream: %v", err)
	}
	wantFirst := "http://fake-hls/0.m3u8"
	if info1.URLs[0] != wantFirst {
		t.Fatalf("first resolve URL = %q, want %q (fake yt-dlp not invoked as expected)", info1.URLs[0], wantFirst)
	}

	info2, err := src.GetStream(ctx, url)
	if err != nil {
		t.Fatalf("second GetStream: %v", err)
	}
	if info2.URLs[0] != wantFirst {
		t.Fatalf("cache miss: second GetStream URL = %q, want cached %q (yt-dlp re-ran)", info2.URLs[0], wantFirst)
	}

	// Confirm the fake yt-dlp ran exactly once.
	b, _ := os.ReadFile(countFile)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if n != 1 {
		t.Fatalf("yt-dlp invoked %d times, want 1 (cache should absorb the second call)", n)
	}
}

// TestGetStreamFailCooldown verifies that a failed resolve starts a cooldown
// during which GetStream fails fast WITHOUT re-running yt-dlp. Without this,
// the manager's retry loop hammers the extractor every few seconds and gets
// the exit IP temp-banned by YouTube.
func TestGetStreamFailCooldown(t *testing.T) {
	dir := t.TempDir()
	countFile := filepath.Join(dir, "count")
	scriptBody := `#!/bin/sh
n=0
[ -f "` + countFile + `" ] && n=$(cat "` + countFile + `")
echo $((n+1)) > "` + countFile + `"
echo "ERROR: [youtube] Sign in to confirm you're not a bot" >&2
exit 1
`
	writeFakeYtDlp(t, dir, scriptBody)

	const url = "https://www.youtube.com/watch?v=abc123"
	src, err := New(map[string]string{"fail_cooldown": "0.05"}) // 50ms cooldown
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	count := func() int {
		b, _ := os.ReadFile(countFile)
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return n
	}

	// First call fails and records the failure. Each GetStream runs yt-dlp
	// twice: the primary call plus the automatic --force-ipv4 fallback.
	if _, err := src.GetStream(ctx, url); err == nil {
		t.Fatal("expected resolve error on first call")
	}
	if n := count(); n != 2 {
		t.Fatalf("yt-dlp invoked %d times on first call, want 2 (primary + ipv4 fallback)", n)
	}

	// Within the cooldown, GetStream must fail WITHOUT running yt-dlp, and
	// the error must carry the remaining cooldown so the manager can pace
	// its retry instead of waking into an active cooldown. The underlying
	// cause (the bot-check stderr) must be retained in Err, not a static
	// message, so /healthz shows it for the whole cooldown window.
	_, err = src.GetStream(ctx, url)
	var rae *source.RetryAfterError
	if !errors.As(err, &rae) {
		t.Fatalf("expected RetryAfterError, got: %v", err)
	}
	if rae.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %s, want > 0", rae.RetryAfter)
	}
	if !strings.Contains(rae.Err.Error(), "Sign in to confirm") {
		t.Fatalf("cooldown error lost the underlying cause: %v", rae.Err)
	}
	if n := count(); n != 2 {
		t.Fatalf("yt-dlp invoked during cooldown: %d, want unchanged 2", n)
	}

	// Wait for the cooldown to expire: poll GetStream until yt-dlp runs
	// again (count 2 -> 4) — a fixed sleep against the 50ms cooldown is
	// flaky on loaded CI. Within the window GetStream fails fast without
	// touching yt-dlp; the first call past it re-runs yt-dlp (primary +
	// fallback) before failing again.
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, _ = src.GetStream(ctx, url)
		if count() >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cooldown never expired: count = %d", count())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := count(); n != 4 {
		t.Fatalf("yt-dlp not re-run after cooldown: %d, want 4 (2 per call)", n)
	}
}

// TestNewFailCooldownValidation verifies fail_cooldown parsing: invalid values
// (unparseable, <=0, NaN, ±Inf, time.Duration overflow) fall back to the 60s
// default instead of failing New(), and absurdly large values are clamped to
// maxFailCooldown.
func TestNewFailCooldownValidation(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
	}{
		{"", defaultFailCooldown},       // empty -> default
		{"300", 300 * time.Second},      // valid
		{"0.05", 50 * time.Millisecond}, // sub-second valid
		{"abc", defaultFailCooldown},    // unparseable -> default
		{"0", defaultFailCooldown},      // "disable cooldown" attempt -> default
		{"-5", defaultFailCooldown},     // negative -> default
		{"NaN", defaultFailCooldown},    // NaN: f > 0 is false -> default
		{"+Inf", defaultFailCooldown},   // +Inf passes f > 0, must be rejected
		{"1e15", maxFailCooldown},       // overflows time.Duration -> clamped
		{"100000", maxFailCooldown},     // > 24h -> clamped
	}
	for _, tc := range cases {
		src, err := New(map[string]string{"fail_cooldown": tc.value})
		if err != nil {
			t.Errorf("New(fail_cooldown=%q): unexpected error: %v", tc.value, err)
			continue
		}
		y := src.(*YouTube)
		if y.failCooldown != tc.want {
			t.Errorf("fail_cooldown=%q: got %s, want %s", tc.value, y.failCooldown, tc.want)
		}
	}
}

// TestGetStreamNoURLStartsCooldown verifies that a URL-less info dict (yt-dlp
// exit 0 with valid JSON but no playable URL) starts the cooldown like any
// other resolve failure — without it the manager would re-run yt-dlp on every
// backoff tick, the exact hammering fail_cooldown exists to prevent.
func TestGetStreamNoURLStartsCooldown(t *testing.T) {
	dir := t.TempDir()
	countFile := filepath.Join(dir, "count")
	scriptBody := `#!/bin/sh
n=0
[ -f "` + countFile + `" ] && n=$(cat "` + countFile + `")
echo $((n+1)) > "` + countFile + `"
echo '{"vcodec":"h264","acodec":"aac","ext":"mp4"}'
`
	writeFakeYtDlp(t, dir, scriptBody)

	src, err := New(map[string]string{"fail_cooldown": "0.05"}) // 50ms cooldown
	if err != nil {
		t.Fatal(err)
	}
	const url = "https://www.youtube.com/watch?v=abc123"
	ctx := context.Background()
	count := func() int {
		b, _ := os.ReadFile(countFile)
		n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return n
	}

	// First call: no-URL output is a failed resolve. It returns a
	// RetryAfterError carrying the full cooldown; exit 0 means no ipv4
	// fallback, so exactly one yt-dlp run.
	_, err = src.GetStream(ctx, url)
	var rae *source.RetryAfterError
	if !errors.As(err, &rae) {
		t.Fatalf("first call: expected RetryAfterError, got: %v", err)
	}
	if !strings.Contains(rae.Err.Error(), "no stream URL") {
		t.Fatalf("first call: error cause = %v, want 'no stream URL'", rae.Err)
	}
	if n := count(); n != 1 {
		t.Fatalf("yt-dlp invoked %d times, want 1 (exit 0 has no ipv4 fallback)", n)
	}

	// Within the cooldown, GetStream must fail WITHOUT running yt-dlp.
	_, err = src.GetStream(ctx, url)
	if !errors.As(err, &rae) {
		t.Fatalf("second call: expected RetryAfterError, got: %v", err)
	}
	if n := count(); n != 1 {
		t.Fatalf("yt-dlp re-run during cooldown: %d, want unchanged 1", n)
	}
}

// TestGetStreamIPv4Fallback verifies the resolve-level auto fallback: when the
// first yt-dlp call fails (a broken IPv6 path, say) and IPv4 is not already
// forced, GetStream retries once with --force-ipv4 and returns the IPv4 result.
func TestGetStreamIPv4Fallback(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "argv")
	// Fake yt-dlp: fail unless invoked with --force-ipv4; log every argv.
	scriptBody := `#!/bin/sh
printf 'ARGV:%s\n' "$*" >> "` + logFile + `"
for a in "$@"; do
  if [ "$a" = "--force-ipv4" ]; then
    echo '{"url":"http://fake-hls/v4.m3u8","vcodec":"h264","acodec":"aac","ext":"mp4"}'
    exit 0
  fi
done
echo "getaddrinfo: no route to host" >&2
exit 1
`
	writeFakeYtDlp(t, dir, scriptBody)

	src, err := New(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := src.GetStream(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("GetStream after fallback: %v", err)
	}
	if info.URLs[0] != "http://fake-hls/v4.m3u8" {
		t.Fatalf("fallback resolve URL = %q, want http://fake-hls/v4.m3u8", info.URLs[0])
	}

	// The second (fallback) invocation must carry --force-ipv4.
	log, _ := os.ReadFile(logFile)
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 {
		t.Fatalf("yt-dlp invoked %d times, want 2 (initial + ipv4 fallback):\n%s", len(lines), log)
	}
	if !strings.Contains(lines[1], "--force-ipv4") {
		t.Fatalf("fallback invocation missing --force-ipv4, got: %s", lines[1])
	}
}

// TestGetStreamIPv4FallbackAlsoFails verifies that when both the initial call
// and the --force-ipv4 retry fail, GetStream surfaces an error mentioning both.
func TestGetStreamIPv4FallbackAlsoFails(t *testing.T) {
	dir := t.TempDir()
	scriptBody := `#!/bin/sh
echo "always broken" >&2
exit 1
`
	writeFakeYtDlp(t, dir, scriptBody)

	src, err := New(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = src.GetStream(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err == nil {
		t.Fatal("GetStream error = nil, want failure after both attempts")
	}
	if !strings.Contains(err.Error(), "always broken") {
		t.Errorf("error = %q, want initial stderr in message", err)
	}
	if !strings.Contains(err.Error(), "ipv4 fallback also failed") {
		t.Errorf("error = %q, want 'ipv4 fallback also failed' note", err)
	}
}

// TestGetStreamCookies verifies that a cookies config value is passed to
// yt-dlp via --cookies.
func TestGetStreamCookies(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "argv")
	scriptBody := `#!/bin/sh
printf 'ARGV:%s\n' "$*" >> "` + logFile + `"
echo '{"url":"http://fake-hls/c.m3u8","vcodec":"h264","acodec":"aac","ext":"mp4"}'
`
	writeFakeYtDlp(t, dir, scriptBody)

	src, err := New(map[string]string{"cookies": "/etc/restream/cookies.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.GetStream(context.Background(), "https://www.youtube.com/watch?v=abc123"); err != nil {
		t.Fatalf("GetStream: %v", err)
	}

	log, _ := os.ReadFile(logFile)
	if !strings.Contains(string(log), "--cookies /etc/restream/cookies.txt") {
		t.Errorf("yt-dlp invocation missing --cookies, got: %s", log)
	}
}
