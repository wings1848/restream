package direct

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestNewRequiresSource verifies that a Direct source needs at least one of
// url or url_file.
func TestNewRequiresSource(t *testing.T) {
	if _, err := New(map[string]string{}); err == nil {
		t.Fatal("expected error for empty config, got nil")
	}
}

// TestGetStreamStaticURL verifies the url-only path returns the URL verbatim
// and validates it.
func TestGetStreamStaticURL(t *testing.T) {
	s, err := New(map[string]string{"url": "https://manifest.googlevideo.com/x.m3u8?expire=1"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.GetStream(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.URLs) != 1 || info.URLs[0] != "https://manifest.googlevideo.com/x.m3u8?expire=1" {
		t.Fatalf("unexpected URLs: %v", info.URLs)
	}
	if info.VideoCodec != "" || info.AudioCodec != "" {
		t.Fatalf("codecs must be empty (auto defaults to copy): %q/%q", info.VideoCodec, info.AudioCodec)
	}
}

// TestGetStreamURLFile verifies the url_file path: the URL is re-read from
// disk on every call, so rotating the file picks up the new URL on the next
// resolve.
func TestGetStreamURLFile(t *testing.T) {
	dir := t.TempDir()
	urlFile := filepath.Join(dir, "stream.url")
	if err := os.WriteFile(urlFile, []byte("https://old.example/live.m3u8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(map[string]string{"url_file": urlFile})
	if err != nil {
		t.Fatal(err)
	}

	info, err := s.GetStream(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.URLs[0] != "https://old.example/live.m3u8" {
		t.Fatalf("unexpected URL: %s", info.URLs[0])
	}

	// Rotate the file; the next resolve must return the new URL.
	if err := os.WriteFile(urlFile, []byte("https://new.example/live.m3u8"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err = s.GetStream(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.URLs[0] != "https://new.example/live.m3u8" {
		t.Fatalf("unexpected URL after rotation: %s", info.URLs[0])
	}
}

// TestGetStreamURLFileMissing verifies a missing url_file is an error, so the
// pipeline backoff-retries until an external refresher writes the file.
func TestGetStreamURLFileMissing(t *testing.T) {
	s, err := New(map[string]string{"url_file": filepath.Join(t.TempDir(), "nope.url")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetStream(context.Background(), ""); err == nil {
		t.Fatal("expected error for missing url_file, got nil")
	}
}

// TestGetStreamRejectsNonHTTP verifies non-http(s) URLs are rejected.
func TestGetStreamRejectsNonHTTP(t *testing.T) {
	s, err := New(map[string]string{"url": "rtmp://push.example/live"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetStream(context.Background(), ""); err == nil {
		t.Fatal("expected error for rtmp URL, got nil")
	}
}
