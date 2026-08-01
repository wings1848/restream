// Package health monitors FFmpeg stderr output to detect stream health issues.
package health

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Status represents the health status of an FFmpeg stream.
type Status int

const (
	// StatusHealthy indicates the stream is producing normal progress output.
	StatusHealthy Status = iota
	// StatusStalled indicates no progress output has been seen for the
	// configured timeout, or the stderr stream has ended.
	StatusStalled
	// StatusError indicates FFmpeg reported a fatal error on stderr.
	StatusError
)

// String returns a human-readable name for the status.
func (s Status) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusStalled:
		return "stalled"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// Stats holds the latest parsed FFmpeg progress statistics.
type Stats struct {
	FPS     float64
	Speed   float64
	Dropped int
	Bitrate string
}

// Regex patterns for parsing FFmpeg progress lines on stderr.
var (
	progressRe = regexp.MustCompile(`fps=\s*([\d.]+).*speed=\s*([\d.]+)x`)
	dropRe     = regexp.MustCompile(`drop=\s*(\d+)`)
	bitrateRe  = regexp.MustCompile(`bitrate=\s*([\d.]+kbits/s)`)
)

// errorPatterns are substrings that indicate a fatal FFmpeg error. The bare
// "Error" substring is intentionally absent: FFmpeg emits transient lines
// containing "Error" during normal HLS operation, and the checker only
// reports StatusError after a burst of fatal lines within errWindow.
var errorPatterns = []string{
	"Connection refused",
	"Server returned 404",
	"Invalid data found",
	"Conversion failed!",
	"Error opening input",
	"Immediate exit requested",
	"Connection reset by peer",
}

const (
	// tailLines is how many recent stderr lines the checker retains for
	// operator diagnosis after a health failure.
	tailLines = 20
	// statsLogInterval is how often periodic stream stats are logged.
	statsLogInterval = 60 * time.Second
	// errThreshold is how many fatal error lines within errWindow are
	// required before StatusError is reported.
	errThreshold = 3
	// errWindow bounds the burst of fatal error lines considered together.
	errWindow = 2 * time.Second
)

// Checker monitors an FFmpeg stderr stream for health issues such as
// stalls, errors, and lost connections.
type Checker struct {
	stalledTimeout time.Duration
	logger         *slog.Logger

	mu          sync.Mutex
	latestStats Stats
	tail        []string
}

// NewChecker creates a new Checker. stalledTimeout specifies how long
// without progress output before the stream is considered stalled.
func NewChecker(stalledTimeout time.Duration, logger *slog.Logger) *Checker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Checker{
		stalledTimeout: stalledTimeout,
		logger:         logger,
	}
}

// LatestStats returns the most recently parsed FFmpeg statistics.
func (c *Checker) LatestStats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latestStats
}

func (c *Checker) updateStats(s Stats) {
	c.mu.Lock()
	c.latestStats = s
	c.mu.Unlock()
}

// Tail returns a copy of the most recent stderr lines seen by the checker.
// It is intended for operator diagnosis after a health failure.
func (c *Checker) Tail() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.tail))
	copy(out, c.tail)
	return out
}

func (c *Checker) recordLine(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.tail) == tailLines {
		copy(c.tail, c.tail[1:])
		c.tail[tailLines-1] = line
	} else {
		c.tail = append(c.tail, line)
	}
}

// Start begins monitoring FFmpeg stderr. It reads from stderr line by line,
// parses FFmpeg progress output, and sends status updates on the returned
// channel when issues are detected. The goroutine exits when the stderr
// reader reaches EOF or the context is cancelled.
func (c *Checker) Start(ctx context.Context, stderr io.Reader) <-chan Status {
	statusCh := make(chan Status, 1)
	go c.monitor(ctx, stderr, statusCh)
	return statusCh
}

// splitOnCRLF is a bufio.SplitFunc that splits on both '\r' and '\n'
// separators. FFmpeg progress output uses '\r' (carriage return) to overwrite
// the previous progress block rather than '\n', so a plain bufio.ScanLines
// never yields a token for a healthy stream. Each progress block is delivered
// as its own token, with any trailing '\r' stripped.
func splitOnCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		case '\n':
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		// Drop a trailing '\r' on the final unterminated token.
		if n := len(data); n > 0 && data[n-1] == '\r' {
			return n, data[:n-1], nil
		}
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}

func (c *Checker) monitor(ctx context.Context, stderr io.Reader, statusCh chan<- Status) {
	defer close(statusCh)

	lineCh := make(chan string, 1)

	// Goroutine to drain stderr line by line via bufio.Scanner. Progress
	// blocks are separated by '\r', so a custom split function is used.
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(stderr)
		scanner.Split(splitOnCRLF)
		// ffmpeg can emit long error dumps; raise the token limit from the
		// 64KiB default to 256KiB.
		scanner.Buffer(make([]byte, 4096), 256*1024)
		for scanner.Scan() {
			select {
			case lineCh <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	stallTimer := time.NewTimer(c.stalledTimeout)
	defer stallTimer.Stop()

	statsTicker := time.NewTicker(statsLogInterval)
	defer statsTicker.Stop()

	lastStatus := StatusHealthy
	var lastLoggedStats Stats
	var statsLogged bool
	var errCount int
	var errLastSeen time.Time

	// nonBlockingSend sends a status to the channel without blocking.
	// It only sends when the status differs from the last sent value
	// to avoid flooding the consumer.
	nonBlockingSend := func(s Status) {
		if s == lastStatus {
			return
		}
		select {
		case statusCh <- s:
			lastStatus = s
			if s != StatusHealthy {
				// On a transition away from healthy, log the recent stderr
				// tail so operators can see ffmpeg's actual error text.
				c.logger.Debug("health status change", "status", s.String(), "stderr_tail", c.Tail())
			}
		default:
		}
	}

	for {
		select {
		case <-ctx.Done():
			return

		case line, ok := <-lineCh:
			if !ok {
				// Scanner exited (EOF or error reading stderr) ->
				// the stream has ended.
				nonBlockingSend(StatusStalled)
				return
			}

			c.logger.Debug("ffmpeg stderr", "line", line)
			c.recordLine(line)

			// Check for known fatal error patterns. A single error line is
			// not trusted: transient "Error"-containing lines are common
			// during HLS operation, so StatusError is only reported after a
			// burst of fatal lines within a short window.
			if isErrorLine(line) {
				now := time.Now()
				if now.Sub(errLastSeen) >= errWindow {
					errCount = 0
				}
				errCount++
				errLastSeen = now
				if errCount >= errThreshold {
					nonBlockingSend(StatusError)
				}
			}

			// Parse FFmpeg progress line for fps, speed, bitrate, and
			// dropped frames.
			if matches := progressRe.FindStringSubmatch(line); matches != nil {
				// Drain and reset the stall timer.
				if !stallTimer.Stop() {
					select {
					case <-stallTimer.C:
					default:
					}
				}
				stallTimer.Reset(c.stalledTimeout)

				stats := Stats{}
				if len(matches) >= 2 {
					if fps, err := strconv.ParseFloat(strings.TrimSpace(matches[1]), 64); err == nil {
						stats.FPS = fps
					}
				}
				if len(matches) >= 3 {
					if speed, err := strconv.ParseFloat(strings.TrimSpace(matches[2]), 64); err == nil {
						stats.Speed = speed
					}
				}
				if bm := bitrateRe.FindStringSubmatch(line); bm != nil {
					stats.Bitrate = bm[1]
				}
				if dm := dropRe.FindStringSubmatch(line); dm != nil {
					if dropped, err := strconv.Atoi(strings.TrimSpace(dm[1])); err == nil {
						stats.Dropped = dropped
					}
				}

				c.updateStats(stats)
				nonBlockingSend(StatusHealthy)
			}

		case <-statsTicker.C:
			// Periodically log the latest progress stats. Only emit when the
			// stats changed since the last tick to avoid spamming identical
			// values.
			stats := c.LatestStats()
			if !statsLogged || stats != lastLoggedStats {
				lastLoggedStats = stats
				statsLogged = true
				c.logger.Info("stream stats",
					"fps", stats.FPS,
					"speed", stats.Speed,
					"bitrate", stats.Bitrate,
					"dropped", stats.Dropped,
				)
			}

		case <-stallTimer.C:
			// No progress output within the configured timeout.
			nonBlockingSend(StatusStalled)
			stallTimer.Reset(c.stalledTimeout)
		}
	}
}

// isErrorLine returns true when line contains any known fatal FFmpeg error
// pattern. Matching is case-insensitive to catch both "Error" and "error".
func isErrorLine(line string) bool {
	lower := strings.ToLower(line)
	for _, pat := range errorPatterns {
		if strings.Contains(lower, strings.ToLower(pat)) {
			return true
		}
	}
	return false
}
