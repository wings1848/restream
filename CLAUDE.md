# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

restream is a Go live-stream relay: it pulls a YouTube live stream (HLS) via yt-dlp and pushes it to Bilibili (RTMP), running 7×24 unattended. Auto-reconnect with exponential backoff, per-stream smart transcode, `/healthz` monitoring, ~50MiB steady state in default (auto+H.264) mode.

## Commands

```bash
go build ./...                # compile everything
go vet ./...                  # static checks
go test ./...                 # all tests (config/ffmpeg/health/youtube)
go test ./ffmpeg/ -run TestBuildCommandForce   # run a single test
gofmt -l .                    # check formatting (must be clean)
CGO_ENABLED=0 go build -o restream .   # static binary
```

Run: `./restream --url <YT_URL> --key <stream_key> --transcode auto` (CLI) or `--config config.yaml`.
Build image with host proxy: `docker build --network host --build-arg HTTP_PROXY=http://127.0.0.1:7897 --build-arg HTTPS_PROXY=http://127.0.0.1:7897 -t restream:clean .`

## Architecture

**Data path — yt-dlp resolves once, ffmpeg pushes directly; yt-dlp is NOT in the download path:**
`source.GetStream` (single `yt-dlp -j` call → HLS URLs + codecs) → `ffmpeg.Pipeline.BuildCommand` (builds the ffmpeg args) → ffmpeg subprocess pulls HLS and pushes RTMP.

- **`source.Source` / `sink.Sink`** interfaces + registry (implement in `init()` + `Register`, blank-import in main.go). Add a platform by implementing these; the youtube impl shells out to yt-dlp.
- **`pipeline.Manager`** owns the lifecycle: resolve → start ffmpeg → health monitor → retry/backoff. Signature `NewManager(cfg, healthCheckTimeout, registry)`. `Run()` loops `runOnce` with exponential backoff; `interval` resets after a long healthy run.
- **`ffmpeg.Pipeline`** builds args. `passThroughCodecs = {h264, aac}` drive per-stream copy vs transcode in auto mode. Per-input HLS pull flags (`-reconnect`, `-rw_timeout`, `-fflags nobuffer+genpts`) are prepended before each `-i`.
- **`health.Checker`** watches ffmpeg stderr. IMPORTANT: ffmpeg progress is separated by `\r`, not `\n` — the checker splits on BOTH via `splitOnCRLF`; a plain newline scanner would false-positive "stalled" on a healthy stream. StatusError requires 3 fatal lines within 2s (a single `Error`-containing line is not fatal).
- **`health.Registry`** + `/healthz` endpoint (`global.http_addr`, default `:8080`): per-pipeline state/uptime/last_error/bitrate/fps/stderr tail. Manager publishes to it via `setHealthState`.
- **`config`**: YAML → `${VAR}` env expansion (an unresolved `${VAR}` fails at load) → per-pipeline defaults → validation. `ffmpeg.transcode` = auto|copy|force; `threads` caps encoder threads; `maxrate` caps transcoded uplink bitrate.

## Gotchas

- YouTube auth in 2026 REQUIRES the **PO Token Provider sidecar** reachable at `127.0.0.1:4416` (bgutil-ytdlp-pot-provider). The compose file uses `network_mode: host` for BOTH services so yt-dlp finds it. Verify: `curl -fsS http://127.0.0.1:4416/ping`.
- yt-dlp must be recent (`pipx install yt-dlp`); distro apt/brew packages are too old for PO-token/n-sig.
- `source.config.format` defaults to `best` and is reliable on live; `bestvideo+bestaudio` is VOD-oriented and often errors on live streams.
- Bilibili gives one full `rtmp://…?streamname=..&key=..&pflag=2`; it must be split into `rtmp_url` (up to `live-bvc/`) and `stream_key` (the `?streamname=…` part). Pasting the whole URL into either field breaks the RTMP handshake.
- `config.yaml` / `.env` are gitignored (they hold the push key) — never commit or push them; `.dockerignore` excludes config.yaml from the build context.
- Changing config requires a container restart (config is mounted `:ro`).
- auto mode copies h264+aac (zero CPU); only vp9/av1/hevc/opus are re-encoded. `threads: 2` ≈ 256MiB transcode memory (default 0 = all cores ≈ 900MiB).
- The YouTube source caches a resolved HLS URL for 10 minutes — rapid reconnects skip the yt-dlp extraction.

## Docs

Detailed user docs live in `docs/` (configuration, deployment, healthz, troubleshooting, extending). README.md / README-zh.md are slim landing pages linking to them.
