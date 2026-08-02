package youtube

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestGetStreamCacheHit verifies that a second GetStream within the cache TTL
// reuses the first resolution instead of re-running yt-dlp. A fake yt-dlp
// script emits a distinct URL on every invocation and counts calls; the second
// GetStream must return the FIRST URL (cache hit), proving yt-dlp ran once.
func TestGetStreamCacheHit(t *testing.T) {
	dir := t.TempDir()
	countFile := filepath.Join(dir, "count")
	script := filepath.Join(dir, "yt-dlp")
	scriptBody := `#!/bin/sh
n=0
[ -f "` + countFile + `" ] && n=$(cat "` + countFile + `")
echo $((n+1)) > "` + countFile + `"
echo "{\"url\":\"http://fake-hls/$n.m3u8\",\"vcodec\":\"h264\",\"acodec\":\"aac\",\"ext\":\"mp4\",\"fps\":30,\"tbr\":4000}"
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

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
	script := filepath.Join(dir, "yt-dlp")
	scriptBody := `#!/bin/sh
n=0
[ -f "` + countFile + `" ] && n=$(cat "` + countFile + `")
echo $((n+1)) > "` + countFile + `"
echo "ERROR: [youtube] Sign in to confirm you're not a bot" >&2
exit 1
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	const url = "https://www.youtube.com/watch?v=abc123"
	src, err := New(map[string]string{"fail_cooldown": "0.05"}) // 50ms cooldown
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// First call fails and records the failure. Each GetStream runs yt-dlp
	// twice: the primary call plus the automatic --force-ipv4 fallback.
	if _, err := src.GetStream(ctx, url); err == nil {
		t.Fatal("expected resolve error on first call")
	}
	b, _ := os.ReadFile(countFile)
	first, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if first != 2 {
		t.Fatalf("yt-dlp invoked %d times on first call, want 2 (primary + ipv4 fallback)", first)
	}

	// Within the cooldown, GetStream must fail WITHOUT running yt-dlp.
	if _, err := src.GetStream(ctx, url); err == nil {
		t.Fatal("expected cooldown error")
	} else if !strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("expected cooldown error, got: %v", err)
	}
	b, _ = os.ReadFile(countFile)
	second, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if second != first {
		t.Fatalf("yt-dlp invoked during cooldown: %d -> %d, want unchanged", first, second)
	}

	// After the cooldown expires, yt-dlp runs again (primary + fallback).
	time.Sleep(60 * time.Millisecond)
	if _, err := src.GetStream(ctx, url); err == nil {
		t.Fatal("expected resolve error after cooldown (fake yt-dlp always fails)")
	}
	b, _ = os.ReadFile(countFile)
	third, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if third != second+2 {
		t.Fatalf("yt-dlp not re-run after cooldown: %d -> %d, want +2", second, third)
	}
}

// TestGetStreamIPv4Fallback verifies the resolve-level auto fallback: when the
// first yt-dlp call fails (a broken IPv6 path, say) and IPv4 is not already
// forced, GetStream retries once with --force-ipv4 and returns the IPv4 result.
func TestGetStreamIPv4Fallback(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "argv")
	script := filepath.Join(dir, "yt-dlp")
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
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

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
	script := filepath.Join(dir, "yt-dlp")
	scriptBody := `#!/bin/sh
echo "always broken" >&2
exit 1
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

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
	script := filepath.Join(dir, "yt-dlp")
	scriptBody := `#!/bin/sh
printf 'ARGV:%s\n' "$*" >> "` + logFile + `"
echo '{"url":"http://fake-hls/c.m3u8","vcodec":"h264","acodec":"aac","ext":"mp4"}'
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

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
