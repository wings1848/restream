# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `/healthz` HTTP status endpoint (`global.http_addr`, default `:8080`) that
  serves a JSON snapshot of every pipeline: state, start time, uptime, last
  error, live bitrate/fps, and the recent FFmpeg stderr tail.
- Stream-URL resolution cache in the YouTube source (10-minute TTL) so a rapid
  reconnect skips the full yt-dlp extraction.
- `ffmpeg.maxrate` to cap the uplink video bitrate of transcoded streams
  (e.g. `6M`), useful on weak connections.
- Per-stream auto-transcode: with `transcode: auto`, video and audio are
  independently copied or re-encoded based on the source codec, so compatible
  streams are no longer needlessly re-encoded.
- `ffmpeg.threads` to limit FFmpeg encoder threads (and thus memory usage).
- `force_ipv4` option for the YouTube source, for setups where only IPv4 is
  reachable.
- YouTube source support for channel `/live` URLs and yt-dlp timeouts.
- Config validation now rejects unresolved `${ENV}` placeholders and validates
  retry fields.
- `bgutil-ytdlp-pot-provider` sidecar for automatic YouTube PO-token
  authentication.

### Changed

- Stream resolution simplified into a single yt-dlp info-dict call that
  returns both the FFmpeg input URLs and the codec metadata.
- Auto-transcode now uses normalized codec identifiers (`avc1.…` → `h264`,
  `mp4a.…` → `aac`), fixing codec detection for FLV passthrough.
- Live HLS pulls use real-time demux flags (`-fflags nobuffer+genpts`,
  reconnect flags, and a bounded read timeout); the `-re` input flag was
  dropped.
- yt-dlp downloads switched to pipe mode for authenticated stream handling.
- Dockerfile: install Node.js for yt-dlp JS signature solving, install the
  latest yt-dlp via pip, run the pot-provider sidecar on the host network.

### Fixed

- Health checker splits FFmpeg progress output on CRLF line endings, which
  previously prevented progress from being parsed for healthy streams.
- Health checker fixes a select race and resets the backoff interval after a
  long healthy session before a quick reconnect.
- Health-check stall timeout now comes from `global.health_check_interval`.
- Health checker/done race window tightened so a clean FFmpeg exit is not
  reported as a spurious stall.
- Live HLS pipe output uses `--downloader ffmpeg`.

## [0.3.0] - 2026-07-25

### Added

- YouTube PO-token (`poToken` / `visitor_data`) authentication via yt-dlp.

### Fixed

- Removed multi-arch QEMU emulation from the release workflow.

## [0.2.0] - 2026-07-25

### Changed

- Bumped `go.mod` to Go 1.22.

### Added

- Proxy support for the YouTube source (passed to yt-dlp via `--proxy`).
- `GOPROXY` setting for Docker builds.

## [0.1.0] - 2026-07-25

### Added

- Project skeleton: `source`/`sink` interfaces and registries, YouTube source,
  Bilibili sink, and YAML config loader.
- Complete restream pipeline: source resolution, FFmpeg transcoding, and
  RTMP push to Bilibili.
- Health checker wired into the pipeline with deduplicated progress handling
  and goroutine-leak fixes.
- Input format selection and output resolution scaling (`ffmpeg.scale`).
- CI/CD workflow (vet, build, Docker image).

[Unreleased]: https://github.com/wings1848/restream/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/wings1848/restream/releases/tag/v0.3.0
[0.2.0]: https://github.com/wings1848/restream/releases/tag/v0.2.0
[0.1.0]: https://github.com/wings1848/restream/releases/tag/v0.1.0
