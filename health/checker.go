// Package health monitors FFmpeg stderr output to detect stream health issues.
package health

import (
	"bufio"
	"context"
	"io"
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

// errorPatterns are substrings that indicate a fatal FFmpeg error.
var errorPatterns = []string{
	"Error",
	"Invalid data found",
	"Connection refused",
	"Server returned 404",
}

// Checker monitors an FFmpeg stderr stream for health issues such as
// stalls, errors, and lost connections.
type Checker struct {
	stalledTimeout time.Duration
	mu             sync.Mutex
	latestStats    Stats
}

// NewChecker creates a new Checker. stalledTimeout specifies how long
// without progress output before the stream is considered stalled.
func NewChecker(stalledTimeout time.Duration) *Checker {
	return &Checker{
		stalledTimeout: stalledTimeout,
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

// Start begins monitoring FFmpeg stderr. It reads from stderr line by line,
// parses FFmpeg progress output, and sends status updates on the returned
// channel when issues are detected. The goroutine exits when the stderr
// reader reaches EOF or the context is cancelled.
func (c *Checker) Start(ctx context.Context, stderr io.Reader) <-chan Status {
	statusCh := make(chan Status, 1)
	go c.monitor(ctx, stderr, statusCh)
	return statusCh
}

func (c *Checker) monitor(ctx context.Context, stderr io.Reader, statusCh chan<- Status) {
	defer close(statusCh)

	lineCh := make(chan string, 1)

	// Goroutine to drain stderr line by line via bufio.Scanner.
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(stderr)
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

	lastStatus := StatusHealthy

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

			// Check for known error patterns in every stderr line.
			if isErrorLine(line) {
				nonBlockingSend(StatusError)
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

		case <-stallTimer.C:
			// No progress output within the configured timeout.
			nonBlockingSend(StatusStalled)
			stallTimer.Reset(c.stalledTimeout)
		}
	}
}

// isErrorLine returns true when line contains any known FFmpeg error pattern.
func isErrorLine(line string) bool {
	for _, pat := range errorPatterns {
		if strings.Contains(line, pat) {
			return true
		}
	}
	return false
}
