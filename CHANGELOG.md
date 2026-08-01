# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-08-01

### Added

- Project skeleton: `source`/`sink` interfaces and registries, YouTube source,
  Bilibili sink, and YAML config loader.
- Complete restream pipeline: source resolution, FFmpeg transcoding, and RTMP
  push to Bilibili.
- Smart transcode: `copy` / `auto` / `force`; in `auto` mode video and audio
  are independently copied or re-encoded based on the source codec, so
  compatible (H.264/AAC) streams pass through untouched.
- `/healthz` HTTP status endpoint (`global.http_addr`, default `:8080`) that
  serves a JSON snapshot of every pipeline: state, start time, uptime, last
  error, live bitrate/fps, and the recent FFmpeg stderr tail.
- Stream-URL resolution cache in the YouTube source (10-minute TTL) so a rapid
  reconnect skips the full yt-dlp extraction.
- `ffmpeg.maxrate` to cap the uplink video bitrate of transcoded streams,
  useful on weak connections.
- `ffmpeg.threads` to limit FFmpeg encoder threads (and thus memory usage).
- `force_ipv4` option for the YouTube source, for setups where only IPv4 is
  reachable.
- Resolve-level IPv4 fallback for the YouTube source: if the initial yt-dlp
  resolution fails (e.g. a broken IPv6 path), it is retried once with
  `--force-ipv4` automatically.
- `source.config.cookies` to pass a Netscape-format `cookies.txt` to yt-dlp
  via `--cookies`, required for streams behind YouTube's "Sign in to confirm
  you're not a bot" check or members-only content.
- Docker image uses **Node.js** as yt-dlp's JS runtime for YouTube n-sig
  solving (was QuickJS, which cannot execute the 2026 n-sig algorithm and
  caused "No video formats found").
- YouTube source support for channel `/live` URLs, proxy support, and yt-dlp
  timeouts.
- Config validation now rejects unresolved `${ENV}` placeholders and validates
  retry fields.
- `bgutil-ytdlp-pot-provider` sidecar for automatic YouTube PO-token
  authentication.
- Input format selection and output resolution scaling (`ffmpeg.scale`).
- CI/CD workflow (vet, build, Docker image) and GHCR image publishing.

### Changed

- `ffmpeg.threads` defaults to `2` (≈ 256 MiB transcode memory) instead of
  ffmpeg's all-cores default. It is a pointer field: omit it for the default,
  or set `0` to request ffmpeg's own default (one thread per CPU core).
- `ffmpeg.maxrate` defaults to `"8M"` (near Bilibili's 8000 kbps upload cap)
  instead of unlimited. Only affects transcoded video; `copy` mode forwards
  the source bitrate unchanged.
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
- Bumped `go.mod` to Go 1.22 and set `GOPROXY` for Docker builds.

### Fixed

- Health checker splits FFmpeg progress output on CRLF line endings, which
  previously prevented progress from being parsed for healthy streams.
- Health checker fixes a select race and resets the backoff interval after a
  long healthy session before a quick reconnect.
- Health-check stall timeout now comes from `global.health_check_interval`.
- Health checker/done race window tightened so a clean FFmpeg exit is not
  reported as a spurious stall.
- Live HLS pipe output uses `--downloader ffmpeg`.
- Removed multi-arch QEMU emulation from the release workflow.

[0.1.0]: https://github.com/wings1848/restream/releases/tag/v0.1.0
