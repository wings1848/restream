// Package ffmpeg builds and runs FFmpeg commands for live stream restreaming.
package ffmpeg

import (
	"context"
	"fmt"
	"os/exec"

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
	streamInfo   *source.StreamInfo
	target       *sink.RTMPTarget
	mode         TranscodeMode
	videoEncoder string
	preset       string
	crf          int
	audioEncoder string
	audioBitrate string
}

// NewPipeline creates a new Pipeline with the given configuration.
func NewPipeline(
	streamInfo *source.StreamInfo,
	target *sink.RTMPTarget,
	mode TranscodeMode,
	videoEncoder, preset string,
	crf int,
	audioEncoder, audioBitrate string,
) *Pipeline {
	return &Pipeline{
		streamInfo:   streamInfo,
		target:       target,
		mode:         mode,
		videoEncoder: videoEncoder,
		preset:       preset,
		crf:          crf,
		audioEncoder: audioEncoder,
		audioBitrate: audioBitrate,
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

	needsTranscode := p.mode == TranscodeForce
	if p.mode == TranscodeAuto {
		needsTranscode = NeedsTranscode(p.streamInfo)
	}

	// When using two inputs, explicitly map video from first and audio
	// from second so FFmpeg picks the correct streams.
	if multiInput {
		args = append(args, "-map", "0:v", "-map", "1:a")
	}

	if needsTranscode {
		args = append(args, "-c:v", p.videoEncoder)
		if p.preset != "" {
			args = append(args, "-preset", p.preset)
		}
		if p.crf > 0 {
			args = append(args, "-crf", fmt.Sprintf("%d", p.crf))
		}
		args = append(args, "-c:a", p.audioEncoder)
		if p.audioBitrate != "" {
			args = append(args, "-b:a", p.audioBitrate)
		}
	} else {
		args = append(args, "-c", "copy")
	}

	args = append(args, "-f", "flv", p.target.FullURL())

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd, nil
}

// NeedsTranscode returns true if the stream codec information indicates
// that transcoding is required (video is not h264 or audio is not aac).
// When codec information is empty it returns false, allowing copy mode
// as the default since we do not know better.
func NeedsTranscode(info *source.StreamInfo) bool {
	if info.VideoCodec != "" && info.VideoCodec != "h264" {
		return true
	}
	if info.AudioCodec != "" && info.AudioCodec != "aac" {
		return true
	}
	return false
}
