package health

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestCRLFProgressResetsStallTimer guards the critical bug where ffmpeg's
// progress output is '\r'-separated (not '\n'), so a plain newline-splitting
// scanner never yields progress and the stall timer fires on a healthy stream.
func TestCRLFProgressResetsStallTimer(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A healthy stream feeding progress blocks separated by '\r'.
	go func() {
		defer pw.Close()
		for i := 0; i < 8; i++ {
			fmt.Fprintf(pw, "frame=%d fps=30.0 speed=1.0x\r", i)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	c := NewChecker(150*time.Millisecond, discardLogger())
	statusCh := c.Start(ctx, pr)

	start := time.Now()
	select {
	case s, ok := <-statusCh:
		if !ok {
			t.Fatalf("status channel closed before any status was read")
		}
		// The only status expected is Stalled after EOF (~400ms). If it arrived
		// significantly sooner, the stall timer fired mid-stream => bug.
		if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
			t.Fatalf("stalled reported after %v (<400ms): CR-separated progress did not reset the stall timer", elapsed)
		}
		if s != StatusStalled {
			t.Fatalf("expected StatusStalled on EOF, got %v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout: no status received")
	}
}

// TestCopyModeProgressResetsStallTimer guards the bug where copy-mode
// progress lines (no fps= field) did not match the progress regex, so the
// stall timer fired on a healthy pass-through stream.
func TestCopyModeProgressResetsStallTimer(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Copy-mode progress: size/time/bitrate/speed, but NO fps=.
	go func() {
		defer pw.Close()
		for i := 0; i < 8; i++ {
			fmt.Fprintf(pw, "size=%6dkB time=00:00:%02d.00 bitrate=1000.0kbits/s speed=1.0x\r", i, i)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	c := NewChecker(150*time.Millisecond, discardLogger())
	statusCh := c.Start(ctx, pr)

	start := time.Now()
	select {
	case s, ok := <-statusCh:
		if !ok {
			t.Fatalf("status channel closed before any status was read")
		}
		if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
			t.Fatalf("stalled reported after %v (<400ms): copy-mode progress did not reset the stall timer", elapsed)
		}
		if s != StatusStalled {
			t.Fatalf("expected StatusStalled on EOF, got %v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout: no status received")
	}
}

// TestSingleErrorLineNotFatal ensures a single fatal-pattern line does not
// kill the stream (requires errThreshold lines within errWindow).
func TestSingleErrorLineNotFatal(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer pw.Close()
		// One "Connection refused" line followed by healthy progress.
		pw.Write([]byte("Connection refused\nframe=1 fps=30.0 speed=1.0x\r"))
		time.Sleep(100 * time.Millisecond)
	}()

	c := NewChecker(200*time.Millisecond, discardLogger())
	statusCh := c.Start(ctx, pr)

	select {
	case s := <-statusCh:
		if s == StatusError {
			t.Fatalf("single error line triggered StatusError")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout: no status received")
	}
}

// TestErrorBurstTriggersError ensures errThreshold fatal lines within
// errWindow DO report StatusError.
func TestErrorBurstTriggersError(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer pw.Close()
		for i := 0; i < 3; i++ {
			pw.Write([]byte("Connection refused\n"))
		}
		time.Sleep(100 * time.Millisecond)
	}()

	c := NewChecker(500*time.Millisecond, discardLogger())
	statusCh := c.Start(ctx, pr)

	select {
	case s := <-statusCh:
		if s != StatusError {
			t.Fatalf("expected StatusError after burst, got %v", s)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout: no status received")
	}
}

// TestEOFReportsStalled ensures an empty/closed stderr reports Stalled.
func TestEOFReportsStalled(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := NewChecker(time.Second, discardLogger())
	statusCh := c.Start(ctx, pr)

	select {
	case s := <-statusCh:
		if s != StatusStalled {
			t.Fatalf("expected StatusStalled on EOF, got %v", s)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout: no status received")
	}
}

// TestStatusString verifies the human-readable status names.
func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		StatusHealthy: "healthy",
		StatusStalled: "stalled",
		StatusError:   "error",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}
