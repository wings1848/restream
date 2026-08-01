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

// hlsPullFlags are per-input options for robust live HLS pulls. They must
// precede each "-i": real-time demuxing (nobuffer), streamed reconnect with a
// bounded delay, and a 10s read timeout so a hung segment fetch can't block
// the pipeline forever (ffmpeg's default HTTP read timeout is infinite).
var hlsPullFlags = []string{
	"-fflags", "nobuffer+genpts",
	"-reconnect", "1",
	"-reconnect_streamed", "1",
	"-reconnect_delay_max", "5",
	"-rw_timeout", "10000000",
}

// BuildCommand constructs an FFmpeg command that reads the stream URLs
// resolved earlier by yt-dlp (Source.GetStream) and pushes to the RTMP
// target. FFmpeg connects to the CDN directly; yt-dlp is not in the
// download path.
func (p *Pipeline) BuildCommand(ctx context.Context) *exec.Cmd {
	args := []string{"-y", "-nostdin"}

	urls := p.streamInfo.URLs
	if len(urls) > 2 {
		urls = urls[:2]
	}
	for _, u := range urls {
		args = append(args, hlsPullFlags...)
		args = append(args, "-i", u)
	}
	if len(urls) >= 2 {
		args = append(args, "-map", "0:v", "-map", "1:a")
	}

	mode := p.ffCfg.Transcode

	videoTranscode := false
	audioTranscode := false
	switch mode {
	case string(TranscodeForce):
		// Force re-encodes both streams regardless of source codecs.
		videoTranscode = true
		audioTranscode = true
	case string(TranscodeAuto):
		videoTranscode = needsVideoTranscode(p.streamInfo)
		audioTranscode = needsAudioTranscode(p.streamInfo)
	}

	if videoTranscode || audioTranscode {
		// Transcode each stream independently so a compatible stream is
		// copied instead of being needlessly re-encoded 24/7.
		if videoTranscode {
			args = append(args, "-c:v", p.ffCfg.VideoEncoder)
			// Limit encoder threads to cap memory usage; ffmpeg defaults to one
			// thread per core, each with its own lookahead/frame buffers.
			if p.ffCfg.Threads > 0 {
				args = append(args, "-threads", fmt.Sprintf("%d", p.ffCfg.Threads))
			}
			if p.ffCfg.Preset != "" {
				args = append(args, "-preset", p.ffCfg.Preset)
			}
			if p.ffCfg.CRF > 0 {
				args = append(args, "-crf", fmt.Sprintf("%d", p.ffCfg.CRF))
			}
			if p.ffCfg.Scale != "" {
				args = append(args, "-vf", fmt.Sprintf("scale=%s", p.ffCfg.Scale))
			}
			// Cap the uplink bitrate for weak connections. Only affects the
			// re-encoded video; bufsize mirrors maxrate (1:1) as a simple default.
			if p.ffCfg.Maxrate != "" {
				args = append(args, "-maxrate", p.ffCfg.Maxrate, "-bufsize", p.ffCfg.Maxrate)
			}
			// Live-encoding flags: low latency, FLV-safe pixel format, and a
			// keyframe cadence (GOP) of 2x the source FPS.
			gop := 60
			if p.streamInfo.FPS > 0 {
				gop = int(2 * p.streamInfo.FPS)
			}
			args = append(args,
				"-tune", "zerolatency",
				"-sc_threshold", "0",
				"-pix_fmt", "yuv420p",
				"-g", fmt.Sprintf("%d", gop),
			)
		} else {
			args = append(args, "-c:v", "copy")
		}

		if audioTranscode {
			args = append(args, "-c:a", p.ffCfg.AudioEncoder)
			if p.ffCfg.AudioBitrate != "" {
				args = append(args, "-b:a", p.ffCfg.AudioBitrate)
			}
		} else {
			args = append(args, "-c:a", "copy")
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
	"aac":  {},
}

// needsVideoTranscode reports whether the source video codec must be
// re-encoded for FLV passthrough. Unknown codecs (empty field) default to
// copy as a safe fallback.
func needsVideoTranscode(info *source.StreamInfo) bool {
	if info.VideoCodec != "" {
		if _, ok := passThroughCodecs[info.VideoCodec]; !ok {
			return true
		}
	}
	return false
}

// needsAudioTranscode reports whether the source audio codec must be
// re-encoded for FLV passthrough. Unknown codecs (empty field) default to
// copy as a safe fallback.
func needsAudioTranscode(info *source.StreamInfo) bool {
	if info.AudioCodec != "" {
		if _, ok := passThroughCodecs[info.AudioCodec]; !ok {
			return true
		}
	}
	return false
}
