package config

import (
	"os"
	"strings"
	"testing"
)

const validYAML = `
global:
  log_level: debug
  health_check_interval: 15
pipelines:
  - name: test
    source:
      type: youtube
      config:
        url: "https://www.youtube.com/watch?v=abc123"
    sink:
      type: bilibili
      config:
        stream_key: "rtmp://ingest.bilibili.com/live/12345"
    retry:
      max_retries: 3
      initial_interval: 2
      max_interval: 30
      backoff_multiplier: 1.5
`

func TestParseValidConfig(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Pipelines) != 1 {
		t.Fatalf("got %d pipelines, want 1", len(cfg.Pipelines))
	}
	p := cfg.Pipelines[0]

	if p.Name != "test" {
		t.Errorf("name = %q, want test", p.Name)
	}
	if p.Source.Type != "youtube" {
		t.Errorf("source.type = %q, want youtube", p.Source.Type)
	}
	if got := p.Source.Config["url"]; got != "https://www.youtube.com/watch?v=abc123" {
		t.Errorf("source.config.url = %q", got)
	}
	if got := p.Sink.Config["stream_key"]; got != "rtmp://ingest.bilibili.com/live/12345" {
		t.Errorf("sink.config.stream_key = %q", got)
	}

	// Defaults applied.
	if p.FFmpeg.Transcode != "auto" {
		t.Errorf("ffmpeg.transcode = %q, want auto (default)", p.FFmpeg.Transcode)
	}
	if p.FFmpeg.VideoEncoder != "libx264" {
		t.Errorf("ffmpeg.video_encoder = %q, want libx264 (default)", p.FFmpeg.VideoEncoder)
	}
	if p.FFmpeg.CRF != 23 {
		t.Errorf("ffmpeg.crf = %d, want 23 (default)", p.FFmpeg.CRF)
	}
	if p.FFmpeg.Threads == nil || *p.FFmpeg.Threads != 2 {
		t.Errorf("ffmpeg.threads = %v, want 2 (default)", p.FFmpeg.Threads)
	}
	if p.FFmpeg.Maxrate != "8M" {
		t.Errorf("ffmpeg.maxrate = %q, want 8M (default)", p.FFmpeg.Maxrate)
	}

	// Explicit retry values kept.
	if p.Retry.MaxRetries != 3 {
		t.Errorf("retry.max_retries = %d, want 3", p.Retry.MaxRetries)
	}
	if p.Retry.InitialInterval != 2 {
		t.Errorf("retry.initial_interval = %d, want 2", p.Retry.InitialInterval)
	}
	if p.Retry.MaxInterval != 30 {
		t.Errorf("retry.max_interval = %d, want 30", p.Retry.MaxInterval)
	}
	if p.Retry.BackoffMultiplier != 1.5 {
		t.Errorf("retry.backoff_multiplier = %v, want 1.5", p.Retry.BackoffMultiplier)
	}

	// Global config.
	if cfg.Global.LogLevel != "debug" {
		t.Errorf("global.log_level = %q, want debug", cfg.Global.LogLevel)
	}
	if cfg.Global.HealthCheckInterval != 15 {
		t.Errorf("global.health_check_interval = %d, want 15", cfg.Global.HealthCheckInterval)
	}
}

func TestParseExpandsEnv(t *testing.T) {
	t.Setenv("RESTREAM_TEST_SOURCE_URL", "https://source.example/live/stream")
	t.Setenv("RESTREAM_TEST_STREAM_KEY", "rtmp://ingest.example/live/secret")

	data := []byte(`
pipelines:
  - name: env-pipe
    source:
      type: rtmp
      config:
        url: "${RESTREAM_TEST_SOURCE_URL}"
    sink:
      type: rtmp
      config:
        stream_key: "${RESTREAM_TEST_STREAM_KEY}"
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	p := cfg.Pipelines[0]
	if got := p.Source.Config["url"]; got != "https://source.example/live/stream" {
		t.Errorf("source.config.url = %q, want expanded value", got)
	}
	if got := p.Sink.Config["stream_key"]; got != "rtmp://ingest.example/live/secret" {
		t.Errorf("sink.config.stream_key = %q, want expanded value", got)
	}
}

func TestParseRejectsUnresolvedEnv(t *testing.T) {
	const missing = "RESTREAM_MISSING_VAR_4f2a91"
	if _, ok := os.LookupEnv(missing); ok {
		t.Skipf("environment variable %s is set; cannot test missing-var behavior", missing)
	}

	data := []byte(`
pipelines:
  - name: bad
    source:
      type: rtmp
      config:
        url: "rtmp://source.example/live"
    sink:
      type: rtmp
      config:
        stream_key: "rtmp://ingest.example/live/${RESTREAM_MISSING_VAR_4f2a91}"
`)
	_, err := Parse(data)
	if err == nil {
		t.Fatal("Parse() error = nil, want unresolved environment variable error")
	}
	if !strings.Contains(err.Error(), "unresolved environment variable ${RESTREAM_MISSING_VAR_4f2a91}") {
		t.Errorf("error = %q, want unresolved env message naming ${RESTREAM_MISSING_VAR_4f2a91}", err)
	}
	if !strings.Contains(err.Error(), "sink.config.stream_key") {
		t.Errorf("error = %q, want offending field sink.config.stream_key", err)
	}
	if !strings.Contains(err.Error(), "pipeline[0]") {
		t.Errorf("error = %q, want pipeline index", err)
	}
}

// validConfig returns a Config that passes Validate.
func validConfig() *Config {
	return &Config{
		Global: GlobalConfig{HealthCheckInterval: 10},
		Pipelines: []Pipeline{
			{
				Name: "valid",
				Source: SourceConfig{
					Type:   "youtube",
					Config: map[string]string{"url": "https://www.youtube.com/watch?v=abc123"},
				},
				Sink: SinkConfig{
					Type:   "bilibili",
					Config: map[string]string{"stream_key": "rtmp://ingest.bilibili.com/live/12345"},
				},
				FFmpeg: FFmpegConfig{Transcode: "auto"},
				Retry: RetryConfig{
					InitialInterval:   5,
					MaxInterval:       60,
					BackoffMultiplier: 2.0,
				},
			},
		},
	}
}

func TestValidateValidConfig(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateRejectsMissingURL(t *testing.T) {
	cfg := validConfig()
	cfg.Pipelines[0].Source.Config = map[string]string{}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "source.config.url is required") {
		t.Fatalf("Validate() error = %v, want missing url error", err)
	}
}

func TestValidateRejectsInvalidTranscode(t *testing.T) {
	cfg := validConfig()
	cfg.Pipelines[0].FFmpeg.Transcode = "h265"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ffmpeg.transcode must be auto|copy|force") {
		t.Fatalf("Validate() error = %v, want invalid transcode error", err)
	}
}

func TestValidateRejectsZeroInitialInterval(t *testing.T) {
	cfg := validConfig()
	cfg.Pipelines[0].Retry.InitialInterval = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "retry.initial_interval must be >= 1") {
		t.Fatalf("Validate() error = %v, want retry.initial_interval error", err)
	}
}

func TestValidateRejectsZeroBackoffMultiplier(t *testing.T) {
	cfg := validConfig()
	cfg.Pipelines[0].Retry.BackoffMultiplier = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "retry.backoff_multiplier must be > 0") {
		t.Fatalf("Validate() error = %v, want retry.backoff_multiplier error", err)
	}
}

func TestValidateRejectsSmallHealthCheckInterval(t *testing.T) {
	cfg := validConfig()
	cfg.Global.HealthCheckInterval = 1
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "global.health_check_interval must be >= 3") {
		t.Fatalf("Validate() error = %v, want global.health_check_interval error", err)
	}
}
