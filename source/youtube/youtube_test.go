package youtube

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
