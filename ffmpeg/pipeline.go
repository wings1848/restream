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
	StreamInfo *source.StreamInfo
	Target     *sink.RTMPTarget
	Transcode  TranscodeMode
	Ffmpeg     config.FFmpegConfig
}

// NewPipeline creates a new Pipeline with the given configuration.
func NewPipeline(cfg PipelineConfig) *Pipeline {
	return &Pipeline{
		streamInfo: cfg.StreamInfo,
		target:     cfg.Target,
		ffCfg:      cfg.Ffmpeg,
	}
}

// BuildCommand constructs the FFmpeg exec.Cmd for this pipeline.
// The caller can use cmd.StderrPipe() to obtain an io.Reader for the
// health checker.
//
// TranscodeCopy builds: ffmpeg -re -i <url> -c copy -f flv <rtmp_full_url>
// TranscodeForce builds: ffmpeg -re -i <url> -c:v <encoder> -preset <preset>
//
//	-crf <crf> -c:a <audio_encoder> -b:a <audio_bitrate> -f flv <rtmp_full_url>
//
// TranscodeAuto uses TranscodeCopy if the source codecs are h264+aac,
// otherwise falls back to TranscodeForce arguments.
func (p *Pipeline) BuildCommand(ctx context.Context) (*exec.Cmd, error) {
	if len(p.streamInfo.URLs) == 0 {
		return nil, fmt.Errorf("no stream URLs provided")
	}

	args := []string{"-re", "-y"}

	// Add input URLs. When multiple URLs are provided the first is treated
	// as the video input and the second as the audio input.
	multiInput := len(p.streamInfo.URLs) >= 2
	for i, u := range p.streamInfo.URLs {
		if i >= 2 {
			break
		}
		args = append(args, "-i", u)
	}

	mode := p.ffCfg.Transcode
	needsTranscode := mode == "force"
	if mode == "auto" {
		needsTranscode = NeedsTranscode(p.streamInfo)
	}

	if multiInput {
		args = append(args, "-map", "0:v", "-map", "1:a")
	}

	if needsTranscode {
		args = append(args, "-c:v", p.ffCfg.VideoEncoder)
		if p.ffCfg.Preset != "" {
			args = append(args, "-preset", p.ffCfg.Preset)
		}
		if p.ffCfg.CRF > 0 {
			args = append(args, "-crf", fmt.Sprintf("%d", p.ffCfg.CRF))
		}
		args = append(args, "-c:a", p.ffCfg.AudioEncoder)
		if p.ffCfg.AudioBitrate != "" {
			args = append(args, "-b:a", p.ffCfg.AudioBitrate)
		}
	} else {
		args = append(args, "-c", "copy")
	}

	args = append(args, "-f", "flv", p.target.FullURL())

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd, nil
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
