# 生产部署指南（当前架构）

本文档基于仓库当前实现（`main`）编写，适用于 `aether-gateway` 的 Docker/裸机部署。

## 1. 架构要点

- 网关进程：`cmd/aether-gateway`
- 传输：WebTransport over HTTP/3（UDP）
- 同端口双栈：
  - UDP：HTTP/3 + WebTransport
  - TCP：TLS/HTTP1.1（`/health`、`/` decoy、Alt-Svc）
- 默认协议入口路径：`/v1/api/sync`

## 2. 一键部署（推荐）

```bash
curl -sL "https://raw.githubusercontent.com/coolapijust/Aether-Realist/main/deploy.sh?$(date +%s)" -o deploy.sh && chmod +x deploy.sh && ./deploy.sh
```

脚本会：
- 生成/更新 `deploy/.env`
- 引导配置 `DOMAIN`、`PSK`、监听端口、Decoy 目录
- 生成自签名证书（若未提供）
- 使用 `deploy/docker-compose.yml` 启动容器

> 备注：从 2026-02-12 起，脚本更新过程不再生成 `*.bak` 备份文件，并会自动清理历史遗留的 `*.bak`。

## 2.1 Native 一键部署（非 Docker）

适用于不希望使用 Docker 的环境（systemd + 本地二进制）。真正一键部署（无需提前 clone 仓库）：

```bash
curl -fsSL "https://raw.githubusercontent.com/coolapijust/Aether-Realist/main/deploy-native.sh?$(date +%s)" | sudo bash -s -- install
```

脚本会：
- 生成/更新 `deploy/.env`
- 自动生成 `deploy/certs/server.{crt,key}`（若不存在）
- 自动准备 `deploy/decoy/index.html`（若不存在）
- `go build` 构建网关并安装到 `/usr/local/bin/aether-gateway`
- 写入 systemd 服务：`/etc/systemd/system/aether-gateway.service`
- 源码目录：`/opt/aether-realist/src`

查看状态/日志：

```bash
sudo systemctl status aether-gateway
sudo journalctl -u aether-gateway -f --no-pager
```

## 3. Docker Compose 关键配置

当前 `deploy/docker-compose.yml` 使用 `network_mode: host`，核心环境变量如下：

- `PSK`：必须，客户端需一致
- `LISTEN_ADDR`：监听地址，通常 `:${PORT}`
- `SSL_CERT_FILE` / `SSL_KEY_FILE`：证书路径（容器内）
- `DECOY_ROOT`：伪装站目录（可选）
- `WINDOW_PROFILE`：`conservative` / `normal` / `aggressive`
- `RECORD_PAYLOAD_BYTES`：数据记录分片大小（默认 `16384`）
- `PERF_DIAG_ENABLE`：性能诊断日志开关（`1` 开启）
- `PERF_DIAG_INTERVAL_SEC`：性能诊断日志周期（默认 `10`）
- `QUIC_*_RECV_WINDOW`：可选，覆盖 `WINDOW_PROFILE` 的窗口值

示例：

```yaml
services:
  aether-backend:
    image: ghcr.io/coolapijust/aether-realist:main
    network_mode: host
    restart: always
    environment:
      - PSK=${PSK}
      - LISTEN_ADDR=:${CADDY_PORT}
      - SSL_CERT_FILE=/certs/server.crt
      - SSL_KEY_FILE=/certs/server.key
      - DECOY_ROOT=/decoy
      - WINDOW_PROFILE=normal
      - RECORD_PAYLOAD_BYTES=16384
      - PERF_DIAG_ENABLE=0
      - PERF_DIAG_INTERVAL_SEC=10
      - QUIC_INITIAL_STREAM_RECV_WINDOW=
      - QUIC_INITIAL_CONN_RECV_WINDOW=
      - QUIC_MAX_STREAM_RECV_WINDOW=
      - QUIC_MAX_CONN_RECV_WINDOW=
    volumes:
      - ./certs:/certs:ro
      - ${DECOY_PATH}:/decoy:ro
```

## 4. 端口与防火墙

必须同时放行同一端口的 TCP + UDP（例如 443）：

- `443/udp`：WebTransport/HTTP3
- `443/tcp`：TLS decoy/health 与 Alt-Svc
- 若使用 80 做跳转/反代，再单独放行 `80/tcp`

很多“无报错但无法连接”问题都是 UDP 端口未放行导致。

## 5. TLS 与证书

网关支持两种模式：

1. 指定证书（生产推荐）
2. 未找到证书时自动生成 10 年自签名证书（测试可用）

支持 `SIGHUP` 热重载证书，可配合 `acme.sh`：

```bash
acme.sh --install-cert -d your-domain.com \
  --cert-file ./deploy/certs/server.crt \
  --key-file  ./deploy/certs/server.key \
  --reloadcmd "docker kill -s HUP aether-gateway-core"
```

### 5.1 可选：一键脚本集成 acme.sh（推荐 standalone）

`deploy.sh` / `deploy-native.sh` 支持可选启用 `acme.sh` 自动签发/续期证书：

- `ACME_ENABLE=1`：启用
- `ACME_MODE=standalone`：HTTP-01 使用 `80/tcp`（无停机，推荐）
- `ACME_MODE=alpn-stop`：TLS-ALPN-01 使用 `443/tcp`（需要短暂停止服务占用 443，作为退路）

注意：`standalone` 要求 `80/tcp` 可用且公网可达（安全组/防火墙放行）。

## 6. 性能参数

### 6.1 `WINDOW_PROFILE`

当前实现值：

- `conservative`: `stream=512KB conn=1.5MB maxStream=2MB maxConn=4MB`
- `normal`: `stream=2MB conn=3MB maxStream=4MB maxConn=8MB`
- `aggressive`: `stream=4MB conn=8MB maxStream=32MB maxConn=48MB`

### 6.2 UDP 缓冲

客户端与网关都会尝试设置 UDP 读写缓冲到 32MB。  
建议同时调高宿主机内核上限：

```bash
sysctl -w net.core.rmem_max=33554432
sysctl -w net.core.wmem_max=33554432
```

### 6.3 Record 分片 A/B

可通过 `RECORD_PAYLOAD_BYTES` 调整数据分片大小：

- `4096`
- `8192`
- `16384`（默认）

建议在同一网络环境下做 3 轮测速对比后固定配置。

### 6.4 QUIC 窗口覆盖 A/B（进阶）

在 `WINDOW_PROFILE` 基础上，可通过以下变量做细调：

- `QUIC_INITIAL_STREAM_RECV_WINDOW`
- `QUIC_INITIAL_CONN_RECV_WINDOW`
- `QUIC_MAX_STREAM_RECV_WINDOW`
- `QUIC_MAX_CONN_RECV_WINDOW`

单位均为字节。留空表示不覆盖，继续使用 `WINDOW_PROFILE` 默认值。

示例（高延迟链路试验）：

```env
WINDOW_PROFILE=aggressive
QUIC_INITIAL_STREAM_RECV_WINDOW=6291456
QUIC_INITIAL_CONN_RECV_WINDOW=12582912
QUIC_MAX_STREAM_RECV_WINDOW=50331648
QUIC_MAX_CONN_RECV_WINDOW=67108864
```

### 6.5 性能诊断日志（定位下行瓶颈）

开启：

```env
PERF_DIAG_ENABLE=1
PERF_DIAG_INTERVAL_SEC=10
```

日志关键字：`[PERF]`。示例字段：

- `down.mbps / up.mbps`：窗口内吞吐
- `down.read_us`：单 record 下行读取平均耗时
- `down.parse_us`：单 record 解析平均耗时
- `up.build_us`：上行封包平均耗时
- `up.write_us`：上行写入平均耗时

### 6.6 一键 A/B 调优脚本

仓库提供 `deploy/perf-tune.sh`，用于脚本化执行：

- 开关性能诊断
- 应用 QUIC 窗口预设
- 自动重启容器
- 抓取 `[PERF]` 日志

示例：

```bash
chmod +x deploy/perf-tune.sh

# 查看当前参数
./deploy/perf-tune.sh status

# 应用基线（仅 WINDOW_PROFILE）
./deploy/perf-tune.sh apply baseline 16384

# 应用下行优化预设 A/B/C
./deploy/perf-tune.sh apply dl-a 16384
./deploy/perf-tune.sh apply dl-b 16384
./deploy/perf-tune.sh apply dl-c 16384

# 连续抓取 120 秒性能日志
./deploy/perf-tune.sh logs 120

# 查看推荐测试矩阵
./deploy/perf-tune.sh matrix
```

## 7. 运行检查

### 7.1 健康检查

- `GET /health` 返回 `200 OK`
- `GET /` 返回 decoy 页面（非协议请求）

### 7.2 日志关键字

- `WebTransport capability: H3 datagrams enabled=true`
- `V5.1 Config: Using WINDOW_PROFILE=...`
- `Starting HTTP/3 (UDP) server on ...`

## 8. 常见故障

1. 客户端连不上但服务端无错误
- 优先排查云防火墙是否放行 UDP 端口。

2. 速度明显偏低
- 确认网关与客户端（core）都在预期的 `WINDOW_PROFILE`。
- 高 RTT 链路建议 `aggressive`。

3. 自签证书连接失败
- 客户端启用 `allow_insecure/skip_verify` 仅用于测试环境。
