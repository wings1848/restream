# restream — YouTube live → Bilibili relay

[![CI](https://img.shields.io/github/actions/workflow/status/wings1848/restream/ci.yml?style=flat&label=CI&color=blue)](https://github.com/wings1848/restream/actions)
[![Go](https://img.shields.io/github/go-mod/go-version/wings1848/restream?style=flat&label=Go&color=blue)](https://go.dev/)
[![License](https://img.shields.io/github/license/wings1848/restream?style=flat&label=License&color=blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/wings1848/restream?style=flat&label=Release&color=blue)](https://github.com/wings1848/restream/releases)
[![中文文档](https://img.shields.io/badge/中文文档-README--zh-blue?style=flat&color=blue)](README-zh.md)

A lightweight, unattended live-stream **mirror / relay**: it pulls a YouTube live stream via `yt-dlp` (HLS), optionally transcodes it, and pushes it to Bilibili (RTMP). Built to run 7×24 with automatic reconnection, health monitoring, and a low memory footprint.

---

## Features

- **YouTube live → Bilibili relay** — pulls HLS with `yt-dlp`, pushes RTMP with FFmpeg.
- **Smart transcode** — `copy` / `auto` / `force`; in `auto` mode H.264 video and AAC audio pass through untouched, only incompatible codecs (VP9, AV1, …) are re-encoded.
- **Auto-reconnect with exponential backoff** — configurable retry budget, intervals, and backoff multiplier.
- **HLS self-healing pull** — FFmpeg `reconnect` flags plus a 10s read timeout, so a hung segment fetch can't wedge the pipeline.
- **`/healthz` status endpoint** — per-pipeline JSON: state, uptime, last error, bitrate, fps, FFmpeg stderr tail.
- **Stream-URL cache** — resolved HLS URLs are reused for 10 minutes, so transient failures reconnect instantly without a full `yt-dlp` re-extraction.
- **Multi-pipeline** — run several relays in one process, independently.
- **Docker multi-stage image** — minimal Alpine runtime with static FFmpeg and a current `yt-dlp` bundled.

## 🚀 Deploy with an AI agent (recommended)

Copy this repo's README link to an AI coding assistant (e.g. Claude Code):

```text
https://github.com/wings1848/restream
```

The AI reads the README, asks you for the deploy mode (docker compose / binary) and the required inputs (YouTube URL, Bilibili push address), then deploys the relay for you.

## Quick start (manual)

### 1. CLI (single pipeline, no config file)

```bash
go build -o restream .
./restream --url "https://www.youtube.com/watch?v=LIVE_ID" \
           --key "YOUR_BILIBILI_STREAM_KEY" \
           --transcode auto
```

Other flags: `--config <path>`, `--log-level debug|info|warn|error`, `--version`.

### 2. Docker Compose (prebuilt image — recommended)

```bash
cp config.yaml.example config.yaml              # edit your config
export BILIBILI_STREAM_KEY="YOUR_KEY"           # or hardcode it in config.yaml
docker compose -f docker-compose.deploy.yml up -d
docker compose -f docker-compose.deploy.yml logs -f
```

This pulls the prebuilt **`ghcr.io/wings1848/restream:latest`** image — no local build needed. To build the image from source instead, use the development `docker-compose.yml` (`docker compose up -d`).

See [docs/deployment.md](docs/deployment.md) for all three ways to run, plus the required **PO Token Provider sidecar** for YouTube.

## Documentation

Detailed guides live in the `docs/` directory:

| Document | What it covers |
|---|---|
| [docs/configuration.md](docs/configuration.md) | Full config reference, transcode modes, the Bilibili stream-key split, env-var expansion. |
| [docs/deployment.md](docs/deployment.md) | CLI / config-file / Docker Compose, prerequisites (Go, FFmpeg, yt-dlp, PO Token Provider sidecar). |
| [docs/healthz.md](docs/healthz.md) | `/healthz` endpoint, JSON fields, state values, monitoring. |
| [docs/troubleshooting.md](docs/troubleshooting.md) | Symptom → cause → fix table. |
| [docs/extending.md](docs/extending.md) | Add a new Source or Sink platform. |

## License

MIT
