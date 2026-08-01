#!/bin/bash
# restream 转播功能测试脚本（在宿主机执行）
# 步骤：pot-provider sidecar → GHCR 镜像检查 → yt-dlp 直播状态验证 → 启动 restream
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"

echo "== [1/4] pot-provider sidecar (host 网络, 127.0.0.1:4416) =="
docker start pot-provider 2>/dev/null || docker run -d --name pot-provider \
  --network host --init --env TOKEN_TTL=6 --restart unless-stopped \
  brainicism/bgutil-ytdlp-pot-provider:latest
sleep 2
curl -s http://127.0.0.1:4416/ping && echo " <- pot-provider OK" || echo "!! pot-provider ping FAILED"

echo
echo "== [2/4] GHCR CI 镜像检查 =="
echo "$(gh auth token)" | docker login ghcr.io -u wings1848 --password-stdin >/dev/null 2>&1
docker pull ghcr.io/wings1848/restream:v0.3.0 2>&1 | tail -2 || echo "!! GHCR v0.3.0 拉取失败（包可能不存在）"

echo
echo "== [3/4] yt-dlp 直播状态验证（经 Clash 代理 7897 + PO Token） =="
docker run --rm --network host restream:local \
  yt-dlp -4 --proxy http://127.0.0.1:7897 --skip-download \
  --print "title=%(title)s | is_live=%(is_live)s | channel=%(channel)s" \
  "https://www.youtube.com/live/tRsQsTMvPNg" 2>&1 | tail -5

echo
echo "== [4/4] 启动 restream 容器 (host 网络) =="
docker rm -f restream-test 2>/dev/null
docker run -d --name restream-test --network host \
  --env-file "$DIR/.env" \
  -e http_proxy=http://127.0.0.1:7897 \
  -e https_proxy=http://127.0.0.1:7897 \
  -v "$DIR/config.yaml:/etc/restream/config.yaml:ro" \
  --restart unless-stopped \
  restream:local --config /etc/restream/config.yaml
echo "restream-test 已启动，15 秒后输出日志…"
sleep 15
docker logs --tail 30 restream-test
