// Package ffmpeg builds and runs FFmpeg commands for live stream restreaming.
package ffmpeg

import (
	"context"
	"fmt"
	"os/exec"

	"restream/config"
	"restream/sink"
	"restream/source"
)

// TranscodeMode controls how FFmpeg handles transcoding.
type TranscodeMode string

const (
	TranscodeAuto  TranscodeMode = "auto"
	TranscodeCopy  TranscodeMode = "copy"
	TranscodeForce TranscodeMode = "force"
)

// Pipeline represents an FFmpeg transcoding pipeline from a source stream
// to an RTMP sink target.
type Pipeline struct {
	streamInfo *source.StreamInfo
	target     *sink.RTMPTarget
	ffCfg      config.FFmpegConfig
}

// PipelineConfig groups parameters for NewPipeline.
type PipelineConfig struct {
	StreamInfo   *source.StreamInfo
	Target       *sink.RTMPTarget
	Transcode    TranscodeMode
	Ffmpeg       config.FFmpegConfig
	SourceCmd    *exec.Cmd // optional: yt-dlp command piping stream to ffmpeg stdin
}

// NewPipeline creates a new Pipeline with the given configuration.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	return &Pipeline{
		streamInfo: cfg.StreamInfo,
		target:     cfg.Target,
		ffCfg:      cfg.Ffmpeg,
	}
}

// BuildPipeCommand builds a yt-dlp → ffmpeg pipeline. yt-dlp handles
// all authentication and outputs the stream to stdout; ffmpeg reads
// from stdin and pushes to the RTMP target. The caller must Start the
// returned ffmpeg command (yt-dlp is already started).
func (p *Pipeline) BuildPipeCommand(ctx context.Context, ytdlpCmd *exec.Cmd) (*exec.Cmd, error) {
	ffCmd := p.buildFfmpegCmd(ctx, true)
	var err error
	ffCmd.Stdin, err = ytdlpCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp stdout pipe: %w", err)
	}
	if err := ytdlpCmd.Start(); err != nil {
		return nil, fmt.Errorf("yt-dlp start: %w", err)
	}
	return ffCmd, nil
}

// BuildCommand constructs a standalone FFmpeg command that reads stream URLs
// directly. For YouTube, prefer BuildPipeCommand instead — it routes via
// yt-dlp which handles authentication.
func (p *Pipeline) BuildCommand(ctx context.Context) (*exec.Cmd, error) {
	return p.buildFfmpegCmd(ctx, false), nil
}

// buildFfmpegCmd constructs the ffmpeg command. When pipeMode is true,
// ffmpeg reads from stdin (pipe:0) instead of URLs.
func (p *Pipeline) buildFfmpegCmd(ctx context.Context, pipeMode bool) *exec.Cmd {
	args := []string{"-re", "-y"}

	if pipeMode {
		args = append(args, "-i", "pipe:0")
	} else {
		multiInput := len(p.streamInfo.URLs) >= 2
		for i, u := range p.streamInfo.URLs {
			if i >= 2 {
				break
			}
			args = append(args, "-i", u)
		}
		if multiInput {
			args = append(args, "-map", "0:v", "-map", "1:a")
		}
	}

	mode := p.ffCfg.Transcode
	needsTranscode := mode == "force"
	if mode == "auto" {
		needsTranscode = NeedsTranscode(p.streamInfo)
	}

	if needsTranscode {
		args = append(args, "-c:v", p.ffCfg.VideoEncoder)
		if p.ffCfg.Preset != "" {
			args = append(args, "-preset", p.ffCfg.Preset)
		}
		if p.ffCfg.CRF > 0 {
			args = append(args, "-crf", fmt.Sprintf("%d", p.ffCfg.CRF))
		}
		if p.ffCfg.Scale != "" {
			args = append(args, "-vf", fmt.Sprintf("scale=%s", p.ffCfg.Scale))
		}
		args = append(args, "-c:a", p.ffCfg.AudioEncoder)
		if p.ffCfg.AudioBitrate != "" {
			args = append(args, "-b:a", p.ffCfg.AudioBitrate)
		}
	} else {
		args = append(args, "-c", "copy")
	}

	args = append(args, "-f", "flv", p.target.FullURL())
	return exec.CommandContext(ctx, "ffmpeg", args...)
}

// passThroughCodecs lists codecs that can be stream-copied directly to FLV
// without re-encoding. FLV container supports H.264 video and AAC audio.
var passThroughCodecs = map[string]struct{}{
	"h264": {},
	"avc":  {}, // shorthand for H.264 AVC
	"aac":  {},
}

// NeedsTranscode returns true when source codecs are known and incompatible
// with FLV passthrough. Unknown codecs (empty field) default to copy as a
// safe fallback.
func NeedsTranscode(info *source.StreamInfo) bool {
	if info.VideoCodec != "" {
		if _, ok := passThroughCodecs[info.VideoCodec]; !ok {
			return true
		}
	}
	if info.AudioCodec != "" {
		if _, ok := passThroughCodecs[info.AudioCodec]; !ok {
			return true
		}
	}
	return false
}
