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

## Quick start

### 1. CLI (single pipeline, no config file)

```bash
go build -o restream .
./restream --url "https://www.youtube.com/watch?v=LIVE_ID" \
           --key "YOUR_BILIBILI_STREAM_KEY" \
           --transcode auto
```

Other flags: `--config <path>`, `--log-level debug|info|warn|error`, `--version`.

### 2. Docker Compose (recommended)

```bash
cp config.yaml.example config.yaml        # edit your config
export BILIBILI_STREAM_KEY="YOUR_KEY"     # or hardcode it in config.yaml
docker compose up -d
docker compose logs -f
```

See [docs/deployment.md](docs/deployment.md) for all three ways to run, plus the required **PO Token Provider sidecar** for YouTube.

## Deploy with an AI agent

The repo ships a **`restream-deploy` skill** ([`skills/restream-deploy-SKILL.md`](skills/restream-deploy-SKILL.md)) that lets an AI coding assistant (e.g. Claude Code) deploy and operate restream for you. When you ask the assistant to set up restream, it will:

1. Ask how you want to deploy — **Docker Compose** or a **binary**;
2. Ask for the required inputs — the YouTube live URL and the Bilibili push address (it will help you split the full `rtmp://…?streamname=…` string into `rtmp_url` + `stream_key`);
3. Set up the config, start the relay, and verify it via `/healthz`.

> Example prompt: *"部署 restream，用 docker compose，YouTube 是 `https://www.youtube.com/live/xxx`，Bilibili 推流地址是 `rtmp://live-push.bilivideo.com/live-bvc/?streamname=…`"*

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
