# Prism Monitor

<p align="center">
  <img src="https://img.shields.io/badge/go-1.23-blue.svg" alt="Go Version">
  <img src="https://img.shields.io/badge/license-MIT-green.svg" alt="License">
  <img src="https://img.shields.io/badge/arch-amd64%20%7C%20arm64%20%7C%20armv7-purple.svg" alt="Architectures">
  <img src="https://img.shields.io/badge/cgo-disabled-brightgreen.svg" alt="CGO Disabled">
  <img src="https://img.shields.io/badge/pkg-com.akiraprism-8B5CF6.svg" alt="Package">
</p>

轻量级、零依赖的 Linux 服务器资源监控 WebUI，采用 Go 语言构建，前后端分离，单二进制内嵌前端 SPA。支持实时 SSE 流式推送与 SQLite 三级历史降采样。

**[English](#english) · [中文文档](#中文文档)**

---

## 中文文档

### 核心特性

- **指标覆盖全**：
  - **CPU**：总体/单核使用率、系统负载（1/5/15m）、**CPU Steal**（虚拟机宿主抢占率检测，超标自动告警）、**iowait**
  - **内存**：总量 / 已用 / 缓存（Buffers + Cached） / 可用
  - **SWAP**：占用率、总量/已用、**换入换出速率（pswpin / pswpout）**、SwapCached
  - **ZRAM**：各压缩内存设备占用、**动态压缩率**（orig / compr）、压缩算法（zstd/lz4/lzo）
  - **磁盘 IO**：实时读写速率、IOPS、**平均等待延迟（await）**
  - **磁盘占用**：按真实挂载点（`ext4`/`xfs`/`btrfs`/`vfat` 等）过滤虚拟设备，展示容量与使用率进度条（>80% 黄、>90% 红）
  - **网络**：各网卡实时上行/下行速率与吞吐图表
- **极致轻量**：纯 Go 编写（无 CGO），常驻内存 **< 20MB**，老旧 Atom D2550 级硬件也能丝滑运行。
- **现代化暗色 UI**：遵循 Linear / Datadog 视觉语言，深色微光主题，内置极简 SVG Sparkline 与 ECharts 趋势图。
- **历史 + 实时一体**：
  - **实时**：SSE（Server-Sent Events）长连接每 2 秒下发一次全量快照，断线自动重连。
  - **历史**：SQLite 纯 Go 驱动内置存储，自动三级降采样：
    - `raw` 层：10s 粒度，保留 2 小时
    - `1m` 聚合层：1 分钟均值/最大/最小，保留 7 天
    - `10m` 聚合层：10 分钟均值/最大/最小，保留 90 天
- **零运行依赖**：前端静态资源通过 `go:embed` 打包进单一二进制文件，无外部 Node 运行时或静态服务器依赖。

### 快速开始

#### 1. Docker 运行（推荐）

```bash
docker run -d \
  --name prism-monitor \
  --restart unless-stopped \
  --network host \
  -v /:/host:ro \
  -v prism-data:/data \
  -e PRISM_PORT=21120 \
  -e PRISM_DB=/data/prism-monitor.db \
  -e PRISM_PROC_ROOT=/host \
  ghcr.io/hybridsakura/prism-monitor:latest
```

或使用 `docker-compose.yml`：

```bash
docker compose up -d
```

打开浏览器访问 `http://<服务器IP>:21120`。

#### 2. Linux 一键脚本安装（带 systemd 服务）

```bash
curl -fsSL https://raw.githubusercontent.com/hybridsakura/prism-monitor/main/packaging/linux/install.sh | sudo bash
```

安装后常用命令：
```bash
systemctl status prism-monitor
journalctl -u prism-monitor -f
systemctl restart prism-monitor
```

#### 3. 下载预编译二进制

从 [Releases](https://github.com/hybridsakura/prism-monitor/releases) 页面下载对应架构的压缩包：

```bash
tar -xzf prism-monitor-linux-amd64.tar.gz
./prism-monitor -port 21120
```

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PRISM_PORT` | `21120` | HTTP 监听端口 |
| `PRISM_HOST` | `0.0.0.0` | 监听地址 |
| `PRISM_DB` | `prism-monitor.db` | SQLite 存储文件路径 |
| `PRISM_PROC_ROOT` | `/` | `/proc` 与 `/sys` 根路径（容器监控宿主时设为 `/host`） |

### API 端点

- `GET /api/v1/now`：最新单次指标快照（JSON）
- `GET /api/v1/stream`：SSE 实时流（`text/event-stream`，每 2s 一帧）
- `GET /api/v1/history?metric=cpu.total&range=1h`：历史指标查询（自动分层）
- `GET /api/v1/mounts`：当前挂载点列表
- `GET /api/v1/disks` / `nets` / `zram`：各维度详细指标
- `GET /api/v1/healthz`：探针接口

### 本地构建与测试

```bash
# 运行全部单测（含 race 竞态检测）
go test -v -race ./...

# 静态检查
go vet ./...

# 交叉编译 Linux 二进制
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o prism-monitor ./cmd/prism-monitor
```

---

## English

### Overview

**Prism Monitor** is a lightweight, zero-dependency server resource monitoring WebUI for Linux servers. Written in Go, it packages a decoupled single-page application (SPA) with embedded frontend assets, offering real-time Server-Sent Events (SSE) streaming and tiered SQLite historical downsampling.

### Highlights

- **Comprehensive Telemetry**:
  - CPU usage, per-core breakdown, system load, **CPU Steal percentage** (hypervisor contention), and **iowait**.
  - Memory: Total, Used, Cached, and Available.
  - SWAP: Usage, swap-in/swap-out rates via `vmstat`, and `SwapCached`.
  - ZRAM: Device usage, **compression ratio**, and active algorithm (`zstd`/`lz4`).
  - Disk I/O: Read/write bandwidth, IOPS, and average await latency.
  - Mounts: Real filesystem filtering with visual usage progress bars.
  - Network: Per-interface throughput.
- **Ultra-lightweight**: Pure Go (zero CGO), resident memory **< 20MB**.
- **Tiered Downsampling**: Raw (10s, 2h) → 1m (7d) → 10m (90d).
- **Single Binary**: All frontend assets embedded via `go:embed`.

### License

MIT License · Copyright (c) 2026 AkiraPrism (`com.akiraprism`).
