# restream — YouTube 直播推流到 Bilibili

restream 是一个轻量级的直播流转发工具，支持从 YouTube 等平台拉取直播流，实时转码后推送到 Bilibili 等目标平台。具备自动重连、指数退避、健康检查等功能，适合 7x24 小时无人值守运行。

## 功能特性

- **直播流转发** — 从 YouTube 拉取 HLS 直播流，推送到 Bilibili RTMP 端点
- **自动重连** — 检测到直播断开或网络异常时自动重连，支持指数退避策略
- **智能转码** — 支持 `copy`（零 CPU 直接转发）和 `force`（强制转码）模式
- **多管道并行** — 一个进程内同时运行多条独立的转推管道
- **可扩展架构** — 通过实现 `Source` / `Sink` 接口即可接入新平台
- **Docker 部署** — 提供多阶段构建镜像，开箱即用
- **日志结构化** — 基于 `slog` 的 JSON/text 结构化日志输出

## 项目结构

```
restream/
├── main.go                 # 入口文件
├── config/
│   └── config.go           # 配置加载、CLI 参数、环境变量展开
├── source/
│   ├── source.go           # Source 接口定义
│   ├── register.go         # Source 注册中心
│   └── youtube/youtube.go  # YouTube 直播源实现
├── sink/
│   ├── sink.go             # Sink 接口定义
│   ├── register.go         # Sink 注册中心
│   └── bilibili/bilibili.go# Bilibili 推流实现
├── ffmpeg/
│   └── pipeline.go         # FFmpeg 管道构建与执行
├── pipeline/
│   └── manager.go          # 单条管道的生命周期管理
└── health/
    └── checker.go          # 流健康监控
```

## 前置要求

### 直接运行（不使用 Docker）

- Go 1.22 或更高版本（仅编译时需要）
- [FFmpeg](https://ffmpeg.org/)（运行时可执行文件，需在 `PATH` 中）
- [yt-dlp](https://github.com/yt-dlp/yt-dlp)（运行时可执行文件，需在 `PATH` 中）
- **PO Token Provider 边车（必需）** — 2026 年起 YouTube 强制要求 PO Token 签名，缺少它拉流会直接失败。restream 的 yt-dlp 插件默认连接 `127.0.0.1:4416`，请确保该地址可达（CLI 与配置文件模式同样依赖它，不只是 Docker）。

> **注意**：发行版自带的 yt-dlp（apt/brew/pacman）通常太旧，无法处理 PO Token 和 n-sig 挑战，请安装最新版：

```bash
# 推荐：pipx 安装最新版 yt-dlp（或 pip install -U yt-dlp）
pipx install yt-dlp
```

FFmpeg 用系统包管理器安装即可：

```bash
# Ubuntu / Debian
sudo apt install ffmpeg

# macOS (Homebrew)
brew install ffmpeg

# Arch Linux
sudo pacman -S ffmpeg
```

非 Docker 直接运行时，用 Docker 以 host 网络启动边车（必须与 yt-dlp 同网络命名空间，才能访问 `127.0.0.1:4416`）：

```bash
docker run -d --name pot-provider --network host \
  --init --env TOKEN_TTL=6 --restart unless-stopped \
  brainicism/bgutil-ytdlp-pot-provider:latest
# 验证：
curl -fsS http://127.0.0.1:4416/ping
```

### 使用 Docker

仅需安装 Docker 和 Docker Compose，无需手动安装 FFmpeg 或 yt-dlp。

## 快速开始

### 方式一：CLI 模式（单管道，无需配置文件）

适合快速测试：

```bash
# 编译
go build -o restream .

# 运行（需要有效的 Bilibili 推流密钥，且 PO Token Provider 边车已在 127.0.0.1:4416 运行，见「前置要求」）
./restream --url "https://www.youtube.com/watch?v=LIVE_VIDEO_ID" \
           --key "你的Bilibili推流密钥" \
           --transcode auto
```

参数说明：
- `--url` — YouTube 直播页面 URL（必填）
- `--key` — Bilibili 推流密钥/码（必填）
- `--transcode` — 转码模式，可选 `auto`、`copy`、`force`（默认 `auto`）
- `--log-level` — 日志级别，可选 `debug`、`info`、`warn`、`error`（默认取 config 的 `global.log_level`，未配置时为 `info`）
- `--version` — 打印版本号后退出

### 方式二：配置文件模式（推荐用于生产环境）

复制示例配置文件并编辑：

```bash
cp config.yaml.example config.yaml
# 编辑 config.yaml，填入直播 URL、推流密钥等
vim config.yaml
```

运行：

```bash
./restream --config config.yaml
```

### 方式三：Docker Compose（推荐）

1. 复制并编辑配置：
   ```bash
   cp config.yaml.example config.yaml
   # 编辑 config.yaml，填入你的配置
   ```

2. 设置环境变量（或直接在 `config.yaml` 中写入明文密钥）：
   ```bash
   export BILIBILI_STREAM_KEY="你的Bilibili推流密钥"
   ```

3. 启动：
   ```bash
   docker compose up -d
   ```
   > `docker-compose.yml` 已包含 pot-provider 服务，且两个服务都使用 `network_mode: host`——yt-dlp 通过 `127.0.0.1:4416` 访问 PO Token 认证（默认 bridge 网络下 `127.0.0.1` 是容器自身，认证会静默失败）。

4. 查看日志：
   ```bash
   docker compose logs -f
   ```

5. 停止：
   ```bash
   docker compose down
   ```

## 配置指南

### 完整配置字段说明

```yaml
global:
  log_level: info              # 日志级别: debug | info | warn | error
  health_check_interval: 10    # 直播“停滞”检测超时（秒）：ffmpeg 该秒数内无进度即判定停滞并重连，建议 3-60
  http_addr: ":8080"           # /healthz 状态端点监听地址（JSON），空字符串 = 禁用

pipelines:
  - name: "youtube-to-bilibili"  # 管道名称（日志中标识用）

    source:
      type: youtube              # 源平台类型（注册的 Source 名称）
      config:
        url: "https://..."       # 直播源 URL（必填）
        format: "best"           # yt-dlp 格式选择器（默认 best；直播用 bestvideo+bestaudio 常不可用，见下）
        proxy: ""                # HTTP/SOCKS 代理（选填，YouTube 被墙时使用）
        force_ipv4: "false"      # 强制 IPv4（代理仅支持 IPv4 时使用）

    sink:
      type: bilibili             # 目标平台类型（注册的 Sink 名称）
      config:
        rtmp_url: "rtmp://..."   # RTMP 推流地址（选填，默认使用 Bilibili 标准端点）
        stream_key: "${KEY}"     # 推流密钥（必填，支持 ${ENV_VAR} 环境变量展开）

    ffmpeg:
      transcode: auto            # 转码模式: auto | copy | force
      video_encoder: libx264     # 视频编码器
      preset: veryfast           # x264 编码预设
      crf: 23                    # 视频质量 (0-51, 越小质量越高)
      scale: ""                  # 分辨率缩放（选填，转码时生效，如 "-1:720" 等比缩放）
      audio_encoder: aac         # 音频编码器
      audio_bitrate: 128k        # 音频码率
      threads: 0                 # 编码线程数（0 = ffmpeg 默认，全核；限制可降内存）
      maxrate: ""                # 上行视频码率上限（弱网用，如 "6M"）；仅转码生效，copy 模式无效

    retry:
      max_retries: 0             # 最大重试次数（0 = 无限重试）
      initial_interval: 5        # 首次重试等待（秒）
      max_interval: 60           # 最大重试等待（秒）
      backoff_multiplier: 2.0    # 退避指数
```

> **关于 `format`（直播流）**：默认 `best`（单个合并后的 HLS 流）对 YouTube 直播最稳。`bestvideo+bestaudio` 是面向点播（VOD）的选择器，直播流上常常没有可用的分离音视频轨，yt-dlp 会直接报错；仅当你确实需要分离轨时才显式设置。

> **关于 `health_check_interval`**：它是直播“卡住”检测的超时时间（秒），不是简单的轮询间隔——ffmpeg 在这段时间内没有输出进度即判定为停滞并触发重连。取值过小会在慢网下误判，过大则断流后恢复慢，建议 3-60。

### Bilibili 推流密钥格式（常见首次配置错误）

Bilibili 直播后台给出的是一整条 RTMP 地址，形如：

```
rtmp://live-push.bilivideo.com/live-bvc/?streamname=abc_123&key=xxx&pflag=2
```

restream 将其拆成两项配置：

| 配置项 | 取值 |
|--------|------|
| `rtmp_url` | 地址到 `live-bvc/` 为止：`rtmp://live-push.bilivideo.com/live-bvc/` |
| `stream_key` | `?` 之后的部分：`streamname=abc_123&key=xxx&pflag=2` |

示例：

```yaml
sink:
  type: bilibili
  config:
    rtmp_url: "rtmp://live-push.bilivideo.com/live-bvc/"
    stream_key: "streamname=abc_123&key=xxx&pflag=2"
```

常见错误：把整条地址塞进 `stream_key`，或把 `?streamname=...` 也写进 `rtmp_url`，导致 RTMP 握手失败。Bilibili 后台“推流码”页面会分别给出“服务器地址（对应 `rtmp_url`）”与“串流密钥（对应 `stream_key`）”，按上述拆分即可。

### 转码模式说明

| 模式 | 说明 | CPU 负载 |
|------|------|----------|
| `copy` | 流复制模式，不进行编解码，直接转发原始流 | 极低（几乎为零） |
| `auto` | 自动模式，检测源流编码格式，仅在必要时转码 | 中等 |
| `force` | 强制转码，使用配置的编码参数重新编码 | 较高 |

### 健康状态端点（healthz）

`global.http_addr`（默认 `:8080`）启动一个 HTTP 端点，返回 JSON 形式的每条管道实时状态，适合 7×24 监控/告警：

```bash
curl -s http://127.0.0.1:8080/healthz
# {"youtube-to-bilibili":{"state":"running","started":"...","uptime":3600,"bitrate":"4561.0kbits/s","fps":30,"stderr_tail":[]}}
```

- `state`：`resolving` / `running` / `backoff` / `stopped`
- `last_error`：最近一次失败原因；`stderr_tail`：ffmpeg 最近 20 行 stderr（排障用）
- 配合 Docker 时，`healthcheck` 可探测 `http://127.0.0.1:8080/healthz`（compose 已用 host 网络）

### 多管道配置

在 `pipelines` 列表中添加多个条目即可并行运行多条管道。每个管道独立运行，互不影响。

```yaml
pipelines:
  - name: "stream-1"
    # ... 管道 1 的配置

  - name: "stream-2"
    # ... 管道 2 的配置
```

## 添加新平台

restream 通过接口抽象实现平台无关性，添加新平台只需实现对应接口即可。

### 添加新的 Source（直播源）

1. 创建包目录，例如 `source/twitch/twitch.go`
2. 实现 `source.Source` 接口：
   ```go
   type Source interface {
       Name() string
       GetStream(ctx context.Context, url string) (*source.StreamInfo, error)
       ValidateURL(url string) error
   }
   ```
3. 在 `init()` 中注册：
   ```go
   func init() {
       source.Register("twitch", New)
   }
   ```
4. 在 `main.go` 中添加匿名导入：
   ```go
   import _ "restream/source/twitch"
   ```

### 添加新的 Sink（推流目标）

1. 创建包目录，例如 `sink/huya/huya.go`
2. 实现 `sink.Sink` 接口：
   ```go
   type Sink interface {
       Name() string
       GetTarget(ctx context.Context, config map[string]string) (*sink.RTMPTarget, error)
       ValidateConfig(config map[string]string) error
   }
   ```
3. 在 `init()` 中注册：
   ```go
   func init() {
       sink.Register("huya", New)
   }
   ```
4. 在 `main.go` 中添加匿名导入。

## 环境变量

| 变量名 | 说明 | 必需 |
|--------|------|------|
| `BILIBILI_STREAM_KEY` | Bilibili 推流密钥 | 配置中使用 `${BILIBILI_STREAM_KEY}` 时需要 |

在 `config.yaml` 中通过 `${VAR_NAME}` 语法引用环境变量，程序启动时自动展开。

## CLI 参数速查

```
Usage of restream:
  --config string     配置文件路径
  --url string        YouTube 直播 URL（无配置文件时的快速启动参数）
  --key string        Bilibili 推流密钥（无配置文件时的快速启动参数）
  --transcode string  转码模式: auto | copy | force
  --log-level string  日志级别: debug | info | warn | error（默认 config 的 global.log_level 或 info）
  --version           打印版本号后退出
```

## 故障排除

### "yt-dlp failed: ..."

- 确认 `yt-dlp` 已安装且在 `PATH` 中（且为最新版，见「前置要求」）
- 检查 YouTube 直播是否真正处于直播状态（非预定、非已结束）
- **确认 PO Token Provider 边车在运行且可被访问** — CLI 模式与配置文件模式同样依赖它（不只是 Docker 模式）。yt-dlp 插件“自动发现”的前提是边车真的监听在 `127.0.0.1:4416`：
  - Docker Compose：两个服务使用 `network_mode: host`（见 `docker-compose.yml`），边车绑定宿主机 `127.0.0.1:4416`
  - 直接运行：边车需监听在 yt-dlp 所在网络命名空间的 `127.0.0.1:4416`，或通过插件 `base_url` 指向实际地址
  - 验证：`curl -fsS http://127.0.0.1:4416/ping`

### "ffmpeg exited with error"

- 确认 `ffmpeg` 已安装且版本 >= 4.0
- 检查 Bilibili 推流密钥是否有效
- 尝试将转码模式设为 `copy` 排除编码问题

### 直播频繁断连

- 检查网络稳定性
- 适当增大 `retry.initial_interval` 和 `retry.max_interval`
- 检查是否被源平台限流（可尝试调整代理或切换线路）

### 容器运行时报 `exec: "yt-dlp": executable file not found in $PATH`

确认使用项目提供的 Dockerfile（其中已安装 yt-dlp）。如果在自己的基础镜像上运行，需要手动安装。

## 许可证

MIT
