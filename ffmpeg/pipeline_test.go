package ffmpeg

import (
	"context"
	"strings"
	"testing"

	"restream/config"
	"restream/sink"
	"restream/source"
)

// buildArgs constructs the ffmpeg command args for a given mode/info/config.
func buildArgs(mode string, info *source.StreamInfo, ff config.FFmpegConfig) []string {
	if ff.Transcode == "" {
		ff.Transcode = mode
	}
	target := &sink.RTMPTarget{URL: "rtmp://target/live/", StreamKey: "?key=x"}
	cmd := NewPipeline(PipelineConfig{
		StreamInfo: info,
		Target:     target,
		Ffmpeg:     ff,
	}).BuildCommand(context.Background())
	return cmd.Args
}

func has(args []string, sub string) bool { return strings.Contains(strings.Join(args, " "), sub) }

func h264AAC() *source.StreamInfo {
	return &source.StreamInfo{URLs: []string{"http://u1"}, VideoCodec: "h264", AudioCodec: "aac", FPS: 30}
}

func TestBuildCommandCopy(t *testing.T) {
	args := buildArgs("copy", h264AAC(), config.FFmpegConfig{})
	if !has(args, "-c copy") {
		t.Errorf("copy mode missing '-c copy': %v", args)
	}
	if !has(args, "-y") || !has(args, "-nostdin") {
		t.Errorf("copy mode missing -y/-nostdin: %v", args)
	}
	if has(args, "libx264") {
		t.Errorf("copy mode must not transcode: %v", args)
	}
}

func TestBuildCommandAutoPassthrough(t *testing.T) {
	// h264 video + aac audio are both FLV-safe => the whole stream is copied
	// with the concise '-c copy' (equivalent to '-c:v copy -c:a copy').
	args := buildArgs("auto", h264AAC(), config.FFmpegConfig{})
	if !has(args, "-c copy") {
		t.Errorf("auto(h264+aac) should copy the whole stream: %v", args)
	}
	if has(args, "libx264") || has(args, "-tune") {
		t.Errorf("auto(h264+aac) must not transcode: %v", args)
	}
}

func TestBuildCommandAutoPartial(t *testing.T) {
	// h264 video + opus audio: video copies, audio is re-encoded to aac.
	info := &source.StreamInfo{URLs: []string{"http://u1"}, VideoCodec: "h264", AudioCodec: "opus"}
	args := buildArgs("auto", info, config.FFmpegConfig{AudioEncoder: "aac", AudioBitrate: "128k"})
	if !has(args, "-c:v copy") {
		t.Errorf("h264 video should copy: %v", args)
	}
	if !has(args, "-c:a aac") {
		t.Errorf("opus audio should transcode to aac: %v", args)
	}
	// Live-encoder flags only apply when video is being encoded.
	if has(args, "-tune") {
		t.Errorf("auto(h264+opus) copies video, should not add live encoder flags: %v", args)
	}
}

func TestBuildCommandForce(t *testing.T) {
	info := &source.StreamInfo{URLs: []string{"http://u1"}, FPS: 30}
	ff := config.FFmpegConfig{VideoEncoder: "libx264", Preset: "veryfast", CRF: 23, AudioEncoder: "aac"}
	args := buildArgs("force", info, ff)
	if !has(args, "-c:v libx264") {
		t.Errorf("force mode missing -c:v libx264: %v", args)
	}
	for _, flag := range []string{"-tune", "zerolatency", "-sc_threshold", "0", "-pix_fmt", "yuv420p", "-g"} {
		if !has(args, flag) {
			t.Errorf("force mode missing live flag %q: %v", flag, args)
		}
	}
	// GOP = 2x FPS (30) => -g 60.
	if !has(args, "-g 60") {
		t.Errorf("force mode expected -g 60 for 30fps, got: %v", args)
	}
}

func TestBuildCommandForceDefaultGOP(t *testing.T) {
	// Unknown FPS falls back to GOP 60.
	info := &source.StreamInfo{URLs: []string{"http://u1"}}
	ff := config.FFmpegConfig{VideoEncoder: "libx264"}
	args := buildArgs("force", info, ff)
	if !has(args, "-g 60") {
		t.Errorf("force mode expected default -g 60, got: %v", args)
	}
}

func TestBuildCommandThreads(t *testing.T) {
	ff := config.FFmpegConfig{VideoEncoder: "libx264", Threads: 2}
	args := buildArgs("force", &source.StreamInfo{URLs: []string{"http://u1"}}, ff)
	if !has(args, "-threads 2") {
		t.Errorf("threads=2 should pass -threads 2: %v", args)
	}
}

func TestBuildCommandTwoInputs(t *testing.T) {
	info := &source.StreamInfo{URLs: []string{"http://u1", "http://u2"}, VideoCodec: "h264", AudioCodec: "aac"}
	args := buildArgs("copy", info, config.FFmpegConfig{})
	if !has(args, "-map 0:v -map 1:a") {
		t.Errorf("two inputs should map 0:v/1:a: %v", args)
	}
	// hlsPullFlags must be emitted per input.
	joined := strings.Join(args, " ")
	if c := strings.Count(joined, "-reconnect 1"); c != 2 {
		t.Errorf("expected -reconnect 1 twice for two inputs, got %d: %v", c, args)
	}
}

func TestBuildCommandSingleInputNoMap(t *testing.T) {
	args := buildArgs("copy", h264AAC(), config.FFmpegConfig{})
	if has(args, "-map") {
		t.Errorf("single input should not add -map: %v", args)
	}
}
