---
name: restream-deploy
description: Deploy, configure, and operate the restream YouTube→Bilibili relay. Use when the user asks to set up, run, rebuild, debug, or monitor restream.
---

# restream — Deploy & Operate

restream is a lightweight Go live-stream relay: it pulls a YouTube live HLS stream (via
yt-dlp), optionally transcodes, and pushes it to a Bilibili RTMP endpoint. It is designed
to run 7×24 unattended with auto-reconnect, exponential backoff, and per-pipeline health
reporting.

## 1. Project overview

- **Architecture:** `Source → pipeline.Manager → ffmpeg.Pipeline → Sink`.
  - `source.Source.GetStream()` shells out to `yt-dlp -j <url>` once and returns HLS/DASH
    input URLs plus codec metadata (video/audio codec, container, fps, bitrate).
  - The `pipeline.Manager` resolves the stream, builds the ffmpeg command, runs it, and on
    failure retries with exponential backoff.
  - `ffmpeg` connects to the CDN directly and pushes to the RTMP sink; **yt-dlp is not in
    the download path** (it only resolves URLs).
  - `sink.FullURL()` = `rtmp_url + stream_key`.
- Multiple pipelines run concurrently in one process; each is independent.
- YouTube PO-Token auth is handled by a `bgutil-ytdlp-pot-provider` sidecar that yt-dlp
  "auto-discovers" at `127.0.0.1:4416` (hard requirement in 2026).

## 2. Key files map

| File | Role |
|------|------|
| `main.go` | Entry point. CLI flags (`--config`, `--url`, `--key`, `--transcode`, `--log-level`, `--version`), structured slog logger, graceful shutdown on SIGINT/SIGTERM, /healthz HTTP server, launches all pipelines concurrently. |
| `config/config.go` | Config structs, defaults, `${ENV_VAR}` expansion, CLI synthesis/override, validation. Env expansion priority: CLI flags > env vars > config file. |
| `source/youtube/youtube.go` | YouTube Source. Runs yt-dlp with precomputed base args + `-j`; parses the info dict for URLs + codecs; caches the last successful resolution for 10 min so rapid reconnects skip yt-dlp. |
| `ffmpeg/pipeline.go` | Builds the ffmpeg command: copy/auto/force transcode logic, HLS pull flags, threads/maxrate/CRF/scale/live-encoder flags. |
| `pipeline/manager.go` | One pipeline's lifecycle: resolve → build → run → health-monitor → retry/backoff. Sets per-pipeline state in the health registry. |
| `health/checker.go` | Parses ffmpeg stderr (fps/speed/bitrate/drop), stall detection, error-burst detection, keeps a 20-line stderr tail. |
| `health/registry.go` | Concurrent per-pipeline status store served by /healthz. |
| `config.yaml.example` | Annotated full config template. |
| `docker-compose.yml` | pot-provider + restream services, both `network_mode: host`. |
| `Dockerfile` | 3-stage: Go build → static ffmpeg download → minimal Alpine runtime with yt-dlp + quickjs. |

## 3. Config cheat-sheet

All paths are relative to a YAML file. Environment variables in `${VAR}` form are expanded
at load time; **an unresolved `${VAR}` now fails at load** with a clear error (see
`checkUnresolvedEnv` in `config/config.go`).

**global** (process-wide):
- `log_level`: `debug | info | warn | error` (default `info`).
- `health_check_interval`: stall-detection timeout in seconds (3–60, validated `>= 3`;
  default `10`). This is the max time ffmpeg can go without progress output before the
  stream is considered stalled and restream reconnects — not a poll interval. Too small
  false-positives on slow networks; too large delays recovery after a drop.
- `http_addr`: /healthz listen address (default `:8080`; empty string disables it).

**pipelines[].source** (`type: youtube`):
- `url`: YouTube live URL (required).
- `format`: yt-dlp format selector (default `best`; empty = `best`). `bestvideo+bestaudio`
  is VOD-oriented and usually errors on live — avoid.
- `proxy`: HTTP/SOCKS proxy for yt-dlp, e.g. `http://127.0.0.1:7897` or `socks5://...`.
  Empty = direct. TUN/VPN mode is preferred (no proxy config needed).
- `force_ipv4`: `"true"`/`"false"` — force IPv4 (useful when the proxy is IPv4-only).

**pipelines[].sink** (`type: bilibili`):
- `rtmp_url`: RTMP ingest address (default `rtmp://live-push.bilivideo.com/live-bvc/`).
- `stream_key`: Bilibili stream key (required; supports `${ENV}` expansion).
- **Critical split:** Bilibili gives one full URL like
  `rtmp://live-push.bilivideo.com/live-bvc/?streamname=abc_123&key=xxx&pflag=2`.
  Put the address **up to `live-bvc/`** in `rtmp_url` and **everything after `?`**
  (`streamname=abc_123&key=xxx&pflag=2`) in `stream_key`. Putting the full URL in
  `stream_key`, or including `?streamname=...` in `rtmp_url`, breaks the RTMP handshake.

**pipelines[].ffmpeg**:
- `transcode`: `auto | copy | force` (default `auto`). `copy` = stream-copy, zero CPU;
  `auto` = copy compatible codecs, transcode only incompatible ones; `force` = re-encode both.
- `video_encoder`: e.g. `libx264` (default; used when transcoding).
- `preset`: x264 preset, default `veryfast` (`ultrafast`…`veryslow`).
- `crf`: 0–51, default `23` (lower = better quality, higher bitrate).
- `scale`: resolution scaling when transcoding, e.g. `-1:720` (empty = no scaling).
- `audio_encoder`: default `aac` (`aac | libmp3lame | libopus | copy`).
- `audio_bitrate`: default `128k`.
- `threads`: encoder threads; `0` = ffmpeg default (one per core, highest memory).
  Limiting to `2` caps transcode memory (~200–256 MiB for 1080p x264).
- `maxrate`: uplink video bitrate cap for weak connections, e.g. `6M` (adds
  `-maxrate 6M -bufsize 6M`). Only applies to re-encoded video; `copy` mode ignores it.

**pipelines[].retry**:
- `max_retries`: max retry attempts; `0` = unlimited (default `0`).
- `initial_interval`: first retry wait in seconds (default `5`).
- `max_interval`: backoff ceiling in seconds (default `60`).
- `backoff_multiplier`: exponential backoff factor (default `2.0`).

## 4. Deployment flows

**Prerequisites (non-Docker):** Go 1.22+ (build only), `ffmpeg` in PATH, latest `yt-dlp`
(via pipx/pip, NOT the distro package), and the pot-provider sidecar reachable at
`127.0.0.1:4416`:
```bash
docker run -d --name pot-provider --network host --init --env TOKEN_TTL=6 --restart unless-stopped brainicism/bgutil-ytdlp-pot-provider:latest
curl -fsS http://127.0.0.1:4416/ping   # verify
```

**Way 1 — CLI quick start (single pipeline, no config file):**
```bash
./restream --url "https://www.youtube.com/watch?v=LIVE_ID" --key "BILIBILI_KEY" --transcode auto
```

**Way 2 — config file:**
```bash
cp config.yaml.example config.yaml   # edit url, stream_key, etc.
./restream --config config.yaml
```

**Way 3 — Docker Compose (recommended for production):**
```bash
cp config.yaml.example config.yaml
export BILIBILI_STREAM_KEY="..."
docker compose up -d
docker compose logs -f
docker compose down
```
Both services (`pot-provider` and `restream`) use `network_mode: host`. This is a **hard
requirement** for YouTube auth in 2026: restream's yt-dlp connects to the pot-provider at
`127.0.0.1:4416`, and on the default bridge network `127.0.0.1` is the container itself so
PO-token auth silently fails. `config.yaml` is mounted `:ro` at `/etc/restream/config.yaml`
— changing config requires `docker compose restart restream`.

## 5. Common operations

- **Build binary:** `CGO_ENABLED=0 go build -o restream .`
- **Rebuild image with host proxy** (proxy line for reaching the outside):
  ```bash
  docker build --network host --build-arg HTTP_PROXY=http://127.0.0.1:7897 --build-arg HTTPS_PROXY=http://127.0.0.1:7897 -t restream:clean .
  ```
- **Test a resolve** (verify the source URL + auth actually yield a stream URL):
  ```bash
  docker run --rm --network host --entrypoint yt-dlp restream:clean --flat-playlist --no-warnings -4 --proxy http://127.0.0.1:7897 -f best -j <live-url>
  ```
  (Drop `--proxy` if using VPN/TUN. `--network host` is required so yt-dlp reaches the pot-provider.)
- **Check health:** `curl -s http://127.0.0.1:8080/healthz` → JSON per pipeline with
  `state` (`resolving`/`running`/`backoff`/`stopped`), `started`, `uptime`, `last_error`,
  `bitrate`, `fps`, and `stderr_tail` (last 20 ffmpeg stderr lines).
- **View logs:** `docker compose logs -f restream`
- **Diagnose "yt-dlp failed":** verify pot-provider reachable:
  `curl -fsS http://127.0.0.1:4416/ping`; ensure yt-dlp is latest (pipx) not the distro
  package; confirm the live is actually live (not scheduled/ended).

## 6. Troubleshooting playbook

| Symptom | Check | Fix |
|---------|-------|-----|
| healthz shows `state: backoff`, `stderr_tail` has "Connection refused" / "Failed to resolve hostname live-push.bilivideo.com" | Bilibili RTMP unreachable or key split wrong | Fix `rtmp_url`/`stream_key` split (see §3); verify network can reach `live-push.bilivideo.com`. |
| ffmpeg `exit status 251` | Push failure | Read `stderr_tail` in healthz or debug logs for the actual error; check key validity / RTMP reachability. |
| Stream stalls or keeps dropping | Network / proxy instability | Consider `maxrate` cap (e.g. `6M`) on weak uplinks, or switch proxy line; `auto` mode copies h264 so CPU is near zero. |
| YouTube auth/bot-check failures | pot-provider down or unreachable | Verify `curl -fsS http://127.0.0.1:4416/ping`; confirm both compose services use `network_mode: host` (bridge breaks it). |
| Bilibili channel flickering / no connect | Key or rtmp_url split wrong, or full URL pasted as stream_key | Re-split per §3; ensure `stream_key` starts with `streamname=...` not `rtmp://`. |

## 7. Key behavioral facts (do not guess differently)

- **auto mode** copies `h264`+`aac` (zero CPU — FLV supports both); it only transcodes
  incompatible codecs (`vp9`/`av1`/`hevc`/`opus`); **force** re-encodes both video and
  audio. In auto mode, each of video/audio is decided independently (a compatible one is
  copied while the other is re-encoded).
- **Transcode memory:** `threads: 2` ≈ 256 MiB; all-core (threads: 0) ≈ 900 MiB; default
  `auto` + h264 source ≈ 50–60 MiB total.
- **URL cache:** a resolved HLS URL is cached 10 min; rapid reconnects skip the full
  yt-dlp run (and its PO-token solve). HLS URLs carry ~6h expiry.
- **Health checker** splits ffmpeg stderr on **both `\r` and `\n`** (ffmpeg progress uses
  `\r` to overwrite), and only reports an error after a burst (≥3) of fatal-pattern lines
  within 2s — transient "Error"-containing lines are ignored.
- **`bestvideo+bestaudio` is VOD-oriented** and often errors on live streams; use `best`.
- **Config reload requires a container restart** — `config.yaml` is mounted `:ro`.
- **Always run `gofmt` and `go vet` and `go test ./...`** before considering a code change
  done; do not skip any of them.

## 8. Safety notes

- `config.yaml` and `.env` are gitignored (secrets); never commit or push them.
- `.dockerignore` excludes `config.yaml` from image builds.
- Prefer env-var expansion (`${BILIBILI_STREAM_KEY}`) over plaintext keys; for CLI usage,
  recommend not pasting `--key` into shell history (e.g. read from env or a prompt).
