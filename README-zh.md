> [**English**](README.md) | **中文** · [查看镜像发布](https://github.com/wings1848/restream/releases)

[![CI](https://img.shields.io/github/actions/workflow/status/wings1848/restream/ci.yml?style=flat&label=CI&color=blue)](https://github.com/wings1848/restream/actions)
[![Go](https://img.shields.io/github/go-mod/go-version/wings1848/restream?style=flat&label=Go&color=blue)](https://go.dev/)
[![License](https://img.shields.io/github/license/wings1848/restream?style=flat&label=License&color=blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/wings1848/restream?style=flat&label=Release&color=blue)](https://github.com/wings1848/restream/releases)
[![中文文档](https://img.shields.io/badge/中文文档-README--zh-blue?style=flat&color=blue)](README-zh.md)

# restream — YouTube 直播推流到 Bilibili

restream 是一个轻量级的直播流转发工具：通过 `yt-dlp` 拉取 YouTube 直播流（HLS），可选转码后推送到 Bilibili（RTMP）。具备自动重连、指数退避、健康监控与低内存占用，适合 7×24 小时无人值守运行。

## 功能特性

- **YouTube → Bilibili 直播转发** — `yt-dlp` 拉流（HLS），FFmpeg 推流（RTMP）
- **智能转码** — `copy` / `auto` / `force` 三种模式；`auto` 模式下 H.264 视频与 AAC 音频直通，仅对不兼容编码（VP9、AV1 等）转码
- **自动重连** — 指数退避，可配置重试次数与间隔
- **HLS 自愈拉流** — FFmpeg `reconnect` 参数 + 10 秒读超时，避免卡死的分片请求拖垮管道
- **`/healthz` 健康端点** — 每条管道的 JSON 状态：state、uptime、last error、bitrate、fps、FFmpeg stderr 尾部
- **流 URL 缓存** — 已解析的 HLS URL 缓存 10 分钟，瞬时故障可直接重连，无需重新解析
- **多管道并行** — 一个进程内同时运行多条独立管道
- **Docker 镜像** — 多阶段构建，内置静态 FFmpeg 与最新 yt-dlp

## 🚀 使用 AI 助手部署（推荐）

把本仓库的 README 链接复制给 AI 编码助手（如 Claude Code）：

```text
https://github.com/wings1848/restream
```

AI 读取 README 后，会先询问你部署方式（docker compose / 二进制）与必要参数（YouTube 地址、Bilibili 推流地址），再替你完成部署。

## 快速开始（手动）

### 方式一：CLI（单管道，无需配置文件）

```bash
go build -o restream .
./restream --url "https://www.youtube.com/watch?v=LIVE_ID" \
           --key "你的Bilibili推流密钥" \
           --transcode auto
```

其他参数：`--config <path>`、`--log-level debug|info|warn|error`、`--version`。

### 方式二：Docker Compose（预构建镜像，推荐）

```bash
cp config.yaml.example config.yaml
export BILIBILI_STREAM_KEY="你的Bilibili推流密钥"
docker compose -f docker-compose.deploy.yml up -d
docker compose -f docker-compose.deploy.yml logs -f
```

直接拉取预构建的 **`ghcr.io/wings1848/restream:latest`** 镜像，无需本地构建。想自行从源码构建镜像时，改用开发用的 `docker-compose.yml`（`docker compose up -d`）。

> compose 中的两个服务均使用 `network_mode: host`，以确保 yt-dlp 能访问 `127.0.0.1:4416` 上的 PO Token 服务。

## Bilibili 推流密钥拆分

Bilibili 后台给出的一整条 RTMP 地址需要拆成两项配置：

```
rtmp://live-push.bilivideo.com/live-bvc/?streamname=abc_123&key=xxx&pflag=2
```

- `rtmp_url` — 地址到 `live-bvc/` 为止：`rtmp://live-push.bilivideo.com/live-bvc/`
- `stream_key` — `?` 之后的部分：`streamname=abc_123&key=xxx&pflag=2`

把整条地址塞进 `stream_key`，或把 `?streamname=...` 也写进 `rtmp_url`，会导致 RTMP 握手失败。

## 文档

详细文档位于 `docs/` 目录（英文）：

- [docs/configuration.md](docs/configuration.md) — 完整配置参考、转码模式、密钥拆分、环境变量展开
- [docs/deployment.md](docs/deployment.md) — CLI / 配置文件 / Docker Compose 三种运行方式与前置要求（含 PO Token 边车）
- [docs/healthz.md](docs/healthz.md) — `/healthz` 端点、JSON 字段与监控
- [docs/troubleshooting.md](docs/troubleshooting.md) — 症状 → 原因 → 解决方案对照表
- [docs/extending.md](docs/extending.md) — 如何新增 Source / Sink 平台

## 许可证

MIT
