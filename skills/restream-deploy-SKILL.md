---
name: restream-deploy
description: Deploy, configure, and operate the restream YouTube→Bilibili live relay. Use when the user wants to set up, run, rebuild, debug, or monitor restream.
---

# restream-deploy

restream 是一个 YouTube 直播 → Bilibili 推流的轻量转播工具（Go，7×24 无人值守，默认占 ~50MiB）。

## 第一步：询问部署方式与参数（必须，不要跳过）

1. **部署方式**：`docker compose`（推荐）还是 `二进制`？
2. **必要参数**：
   - YouTube 直播 URL（`url`）
   - Bilibili 推流地址：平台给的是整条 `rtmp://live-push.bilivideo.com/live-bvc/?streamname=..&key=..&pflag=2`，**必须拆分**为：
     - `rtmp_url` = 到 `live-bvc/` 为止（`rtmp://live-push.bilivideo.com/live-bvc/`）
     - `stream_key` = `?` 之后部分（`streamname=..&key=..&pflag=2`）
   - 转码模式（默认 `auto`）
   - 可选：`threads`（转码线程，建议 2）、`maxrate`（弱网码率上限）、`proxy`/`force_ipv4`
3. 若用户缺参数，引导其从 Bilibili 直播后台「推流码」页获取。

## 部署：docker compose（推荐）

1. 前置：Docker。**PO Token Provider 边车必须可达**（yt-dlp 插件连 `127.0.0.1:4416`）；compose 已内置该服务，且两服务都用 `network_mode: host`。
2. `cp config.yaml.example config.yaml`，填入 `url` / `rtmp_url` / `stream_key`。
3. 推流密钥用环境变量：`export BILIBILI_STREAM_KEY=...`（或直接写 config.yaml）。
4. `docker compose up -d` → `docker compose logs -f`。
5. 验证：`curl -s http://127.0.0.1:8080/healthz`（`state: running` 即正常）。

## 部署：二进制

1. 前置：Go 1.22+、FFmpeg、`pipx install yt-dlp`（发行版 apt/brew 太旧，无法过 PO-token/n-sig）、运行边车：
   ```bash
   docker run -d --name pot-provider --network host --init --env TOKEN_TTL=6 --restart unless-stopped brainicism/bgutil-ytdlp-pot-provider:latest
   curl -fsS http://127.0.0.1:4416/ping   # 必须返回 OK
   ```
2. 编译：`CGO_ENABLED=0 go build -o restream .`
3. 快速测试：`./restream --url <YT_URL> --key <stream_key> --transcode auto`
4. 正式：`cp config.yaml.example config.yaml` + `./restream --config config.yaml`

## 关键配置

- `global.health_check_interval`：停滞检测超时（3-60s，默认 10）
- `global.http_addr`：`/healthz` 地址（默认 `:8080`，空=禁用）
- `ffmpeg.transcode`：`auto|copy|force`。**auto 对 h264+aac 直接 copy（零 CPU）**，仅 vp9/av1/hevc/opus 转码
- `ffmpeg.threads`：转码线程（0=全核 ≈900MiB；2 ≈256MiB）
- `ffmpeg.maxrate`：弱网码率上限（如 `"6M"`），仅转码生效
- `source.config.format`：默认 `best`；`bestvideo+bestaudio` 在直播流常不可用（报错）
- 配置中未解析的 `${VAR}` 会在启动时报错

## 构建/验证命令

- 测试：`go test ./...`、`go vet ./...`、`gofmt -l .`
- 重建镜像（带宿主代理）：`docker build --network host --build-arg HTTP_PROXY=http://127.0.0.1:7897 --build-arg HTTPS_PROXY=http://127.0.0.1:7897 -t restream:clean .`
- 解析验证：`docker run --rm --network host --entrypoint yt-dlp restream:clean --flat-playlist --no-warnings -f best -j <url>`

## 排障

- `/healthz` 显示 `backoff` + stderr_tail 含 "Connection refused"/"exit 251" → Bilibili RTMP 不可达，或 `rtmp_url`/`stream_key` 拆分错误
- yt-dlp 报 bot/认证 → pot-provider 未运行或不可达（`curl /ping`）
- 直播断流抖动 → 换代理线路；转码场景设 `maxrate` 压码率
- 改配置需重启容器（config 挂载 `:ro`）

## 安全

- `config.yaml` / `.env` 已 gitignore（含推流密钥），**不要提交或 push**
- 推荐用 `${BILIBILI_STREAM_KEY}` 环境变量，避免明文写入 config
