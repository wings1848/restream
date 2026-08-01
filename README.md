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

- Go 1.26 或更高版本（仅编译时需要）
- [FFmpeg](https://ffmpeg.org/)（运行时可执行文件，需在 `PATH` 中）
- [yt-dlp](https://github.com/yt-dlp/yt-dlp)（运行时可执行文件，需在 `PATH` 中）

安装 FFmpeg 和 yt-dlp：

```bash
# Ubuntu / Debian
sudo apt install ffmpeg yt-dlp

# macOS (Homebrew)
brew install ffmpeg yt-dlp

# Arch Linux
sudo pacman -S ffmpeg yt-dlp
```

### 使用 Docker

仅需安装 Docker 和 Docker Compose，无需手动安装 FFmpeg 或 yt-dlp。

## 快速开始

### 方式一：CLI 模式（单管道，无需配置文件）

适合快速测试：

```bash
# 编译
go build -o restream .

# 运行（需要有效的 Bilibili 推流密钥）
./restream --url "https://www.youtube.com/watch?v=LIVE_VIDEO_ID" \
           --key "你的Bilibili推流密钥" \
           --transcode auto
```

参数说明：
- `--url` — YouTube 直播页面 URL（必填）
- `--key` — Bilibili 推流密钥/码（必填）
- `--transcode` — 转码模式，可选 `auto`、`copy`、`force`（默认 `auto`）
- `--log-level` — 日志级别，可选 `debug`、`info`、`warn`、`error`（默认 `info`）

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
  health_check_interval: 10    # 健康检查间隔（秒）

pipelines:
  - name: "youtube-to-bilibili"  # 管道名称（日志中标识用）

    source:
      type: youtube              # 源平台类型（注册的 Source 名称）
      config:
        url: "https://..."       # 直播源 URL（必填）
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
      audio_encoder: aac         # 音频编码器
      audio_bitrate: 128k        # 音频码率
      threads: 0                 # 编码线程数（0 = ffmpeg 默认，全核；限制可降内存）

    retry:
      max_retries: 0             # 最大重试次数（0 = 无限重试）
      initial_interval: 5        # 首次重试等待（秒）
      max_interval: 60           # 最大重试等待（秒）
      backoff_multiplier: 2.0    # 退避指数
```

### 转码模式说明

| 模式 | 说明 | CPU 负载 |
|------|------|----------|
| `copy` | 流复制模式，不进行编解码，直接转发原始流 | 极低（几乎为零） |
| `auto` | 自动模式，检测源流编码格式，仅在必要时转码 | 中等 |
| `force` | 强制转码，使用配置的编码参数重新编码 | 较高 |

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
  --log-level string  日志级别: debug | info | warn | error（默认 "info"）
```

## 故障排除

### "yt-dlp failed: ..."

- 确认 `yt-dlp` 已安装且在 `PATH` 中
- 检查 YouTube 直播是否真正处于直播状态（非预定、非已结束）
- 确认 pot-provider 容器在运行（`docker ps` 查看），PO Token 认证依赖它

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
