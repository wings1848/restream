// Package health monitors FFmpeg stderr output to detect stream health issues.
package health

import (
	"sync"
	"time"
)

// PipelineState describes the runtime phase of a pipeline.
type PipelineState string

const (
	StateResolving PipelineState = "resolving" // resolving the source stream
	StateRunning   PipelineState = "running"   // ffmpeg is pushing
	StateBackoff   PipelineState = "backoff"   // ffmpeg failed, waiting to retry
	StateStopped   PipelineState = "stopped"   // exited cleanly / not started
)

// PipelineStatus is a snapshot of one pipeline's health, served by the
// /healthz HTTP endpoint.
type PipelineStatus struct {
	State      PipelineState `json:"state"`
	Started    time.Time     `json:"started"`
	Uptime     time.Duration `json:"uptime"`
	LastError  string        `json:"last_error,omitempty"`
	Bitrate    string        `json:"bitrate,omitempty"`
	FPS        float64       `json:"fps,omitempty"`
	StderrTail []string      `json:"stderr_tail,omitempty"`
}

// Registry aggregates per-pipeline health status for the HTTP endpoint.
// It is safe for concurrent use.
type Registry struct {
	mu        sync.Mutex
	pipelines map[string]PipelineStatus
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{pipelines: make(map[string]PipelineStatus)}
}

// Set updates the status snapshot for a named pipeline.
func (r *Registry) Set(name string, s PipelineStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pipelines[name] = s
}

// Snapshot returns a copy of all pipeline statuses keyed by pipeline name.
func (r *Registry) Snapshot() map[string]PipelineStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]PipelineStatus, len(r.pipelines))
	for k, v := range r.pipelines {
		out[k] = v
	}
	return out
}
