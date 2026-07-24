// Package config handles loading and merging configuration from
// CLI flags, environment variables, and YAML config files.
//
// Priority (highest first): CLI flags > environment variables > config file.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level application configuration.
type Config struct {
	Global    GlobalConfig `yaml:"global"`
	Pipelines []Pipeline   `yaml:"pipelines"`
}

// GlobalConfig holds process-wide settings.
type GlobalConfig struct {
	LogLevel             string `yaml:"log_level"`
	HealthCheckInterval  int    `yaml:"health_check_interval"`
}

// Pipeline describes a single source→sink restream pipeline.
type Pipeline struct {
	Name   string       `yaml:"name"`
	Source SourceConfig `yaml:"source"`
	Sink   SinkConfig   `yaml:"sink"`
	FFmpeg FFmpegConfig `yaml:"ffmpeg"`
	Retry  RetryConfig  `yaml:"retry"`
}

// SourceConfig selects a registered source and provides its options.
type SourceConfig struct {
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

// SinkConfig selects a registered sink and provides its options.
type SinkConfig struct {
	Type   string            `yaml:"type"`
	Config map[string]string `yaml:"config"`
}

// FFmpegConfig controls encoding behaviour.
type FFmpegConfig struct {
	Transcode    string `yaml:"transcode"`     // auto | copy | force
	VideoEncoder string `yaml:"video_encoder"` // libx264, libx265, ...
	Preset       string `yaml:"preset"`        // veryfast, medium, ...
	CRF          int    `yaml:"crf"`           // 18-28
	AudioEncoder string `yaml:"audio_encoder"` // aac, libmp3lame, ...
	AudioBitrate string `yaml:"audio_bitrate"` // 128k, 192k, ...
}

// RetryConfig controls exponential-backoff reconnection.
type RetryConfig struct {
	MaxRetries       int     `yaml:"max_retries"`       // 0 = unlimited
	InitialInterval  int     `yaml:"initial_interval"`  // seconds
	MaxInterval      int     `yaml:"max_interval"`       // seconds
	BackoffMultiplier float64 `yaml:"backoff_multiplier"`
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Global: GlobalConfig{
			LogLevel:            "info",
			HealthCheckInterval: 10,
		},
	}
}

// expandEnv replaces ${VAR} placeholders with their environment values.
// Unknown variables are preserved as-is (${VAR}).
func expandEnv(s string) string {
	return os.Expand(s, func(name string) string {
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return "${" + name + "}"
	})
}

// expandConfig recursively walks a map[string]string and expands ${ENV}
// placeholders in every value.
func expandConfig(m map[string]string) {
	for k, v := range m {
		m[k] = expandEnv(v)
	}
}

// Load reads a YAML config file at path, expands environment variables,
// applies defaults, and returns the parsed Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}
	return Parse(data)
}

// Parse parses YAML bytes into a Config with defaults and env expansion.
func Parse(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Expand env vars and apply per-pipeline defaults.
	for i := range cfg.Pipelines {
		expandConfig(cfg.Pipelines[i].Source.Config)
		expandConfig(cfg.Pipelines[i].Sink.Config)
		cfg.Pipelines[i].applyDefaults()
	}

	return &cfg, nil
}

// applyDefaults fills in zero-value fields with sensible values.
func (p *Pipeline) applyDefaults() {
	if p.FFmpeg.Transcode == "" {
		p.FFmpeg.Transcode = "auto"
	}
	if p.FFmpeg.VideoEncoder == "" {
		p.FFmpeg.VideoEncoder = "libx264"
	}
	if p.FFmpeg.Preset == "" {
		p.FFmpeg.Preset = "veryfast"
	}
	if p.FFmpeg.CRF == 0 {
		p.FFmpeg.CRF = 23
	}
	if p.FFmpeg.AudioEncoder == "" {
		p.FFmpeg.AudioEncoder = "aac"
	}
	if p.FFmpeg.AudioBitrate == "" {
		p.FFmpeg.AudioBitrate = "128k"
	}
	if p.Retry.InitialInterval == 0 {
		p.Retry.InitialInterval = 5
	}
	if p.Retry.MaxInterval == 0 {
		p.Retry.MaxInterval = 60
	}
	if p.Retry.BackoffMultiplier == 0 {
		p.Retry.BackoffMultiplier = 2.0
	}
}

// Override applies CLI flag overrides to the config.
// urlOverride and keyOverride are the top-level flags; an empty string
// means "no override".
func (c *Config) Override(urlOverride, streamKeyOverride, transcodeOverride string) {
	if len(c.Pipelines) == 0 {
		return
	}
	p := &c.Pipelines[0]
	if urlOverride != "" {
		if p.Source.Config == nil {
			p.Source.Config = map[string]string{}
		}
		p.Source.Config["url"] = urlOverride
	}
	if streamKeyOverride != "" {
		if p.Sink.Config == nil {
			p.Sink.Config = map[string]string{}
		}
		p.Sink.Config["stream_key"] = streamKeyOverride
	}
	if transcodeOverride != "" {
		p.FFmpeg.Transcode = transcodeOverride
	}
}

// Validate checks the configuration for missing or invalid values.
func (c *Config) Validate() error {
	if len(c.Pipelines) == 0 {
		return fmt.Errorf("at least one pipeline must be defined")
	}
	for i, p := range c.Pipelines {
		if p.Name == "" {
			return fmt.Errorf("pipeline[%d]: name is required", i)
		}
		if p.Source.Type == "" {
			return fmt.Errorf("pipeline[%d] (%s): source.type is required", i, p.Name)
		}
		if p.Sink.Type == "" {
			return fmt.Errorf("pipeline[%d] (%s): sink.type is required", i, p.Name)
		}
		if p.Source.Config == nil || p.Source.Config["url"] == "" {
			return fmt.Errorf("pipeline[%d] (%s): source.config.url is required", i, p.Name)
		}
		if p.Sink.Config == nil || p.Sink.Config["stream_key"] == "" {
			return fmt.Errorf("pipeline[%d] (%s): sink.config.stream_key is required", i, p.Name)
		}
		if t := p.FFmpeg.Transcode; t != "auto" && t != "copy" && t != "force" {
			return fmt.Errorf("pipeline[%d] (%s): ffmpeg.transcode must be auto|copy|force, got %q", i, p.Name, t)
		}
	}
	return nil
}

// CLIFlags holds the parsed command-line values.
type CLIFlags struct {
	ConfigPath string
	URL        string
	StreamKey  string
	Transcode  string
	LogLevel   string
}

// LoadWithFlags loads config from file (when set) then applies CLI overrides.
// When no config file is provided and URL+key are given, a minimal
// single-pipeline config is synthesized.
func LoadWithFlags(flags *CLIFlags) (*Config, error) {
	var cfg *Config

	if flags.ConfigPath != "" {
		var err error
		cfg, err = Load(flags.ConfigPath)
		if err != nil {
			return nil, err
		}
	} else if flags.URL != "" && flags.StreamKey != "" {
		// Synthesize a minimal config from CLI flags with defaults.
		p := Pipeline{
			Name: "cli-pipeline",
			Source: SourceConfig{
				Type:   "youtube",
				Config: map[string]string{"url": flags.URL},
			},
			Sink: SinkConfig{
				Type:   "bilibili",
				Config: map[string]string{"stream_key": flags.StreamKey},
			},
		}
		p.applyDefaults()
		cfg = &Config{
			Global:    DefaultConfig().Global,
			Pipelines: []Pipeline{p},
		}
	} else {
		return nil, fmt.Errorf(
			"provide either --config <path> or both --url and --key",
		)
	}

	// Apply CLI overrides on top.
	if flags.LogLevel != "" {
		cfg.Global.LogLevel = flags.LogLevel
	}
	cfg.Override(flags.URL, flags.StreamKey, flags.Transcode)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}


