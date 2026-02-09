# 生产部署指南

本文档详细说明了 Aether-Realist V5 版本的生产级部署方案，重点关注高可用性、安全防御与自动化 TLS 管理。

---

## 1. 核心镜像部署 (Docker)

Aether-Realist 提供了高性能的 Docker 镜像，内置 HTTP/3 WebTransport 支持与内存优化机制。V5 版本引入了更强的抗重放机制与 Session 自动轮换。

### 生产推荐：Docker Compose 编排

建议采用以下编排方案，它集成了自动化 TLS 获取、伪装站点以及主动探测防御。

```yaml
version: '3.8'

services:
  aether-gateway:
    image: ghcr.io/coolapijust/aether-rea:latest
    container_name: aether-gateway
    restart: always
    environment:
      - PSK=${PSK}
      - DOMAIN=${DOMAIN}
    ports:
      - "8080:8080/udp"
      - "8080:8080/tcp"
    volumes:
      - ./certs:/certs:ro
    # 生产模式建议直接绑定正式证书
    command: -cert /certs/fullchain.pem -key /certs/privkey.pem -psk "${PSK}"
```

### 运行时参数说明
- `-psk`: **必须**。所有客户端连接必须匹配此密钥。
- `-cert / -key`: 证书与私钥路径。若未提供，网关将自动降级为自签名证书模式，适用于测试或负载均衡器后端。
- `-listen`: 监听地址，默认为 `:8080`。

---

## 2. 安全与性能加固 (V5 特性)

### 2.1 抗重放与 0-RTT
V5 协议默认开启 **0-RTT** 支持。为了确保安全性，服务端实现了严格的 **单调计数器（Monotonic Counter）** 校验。
- **运维建议**：确保服务器系统时间同步（使用 NTP），虽然 V5 主要依赖计数器防止重放，但 30s 的时间窗口校验依然作为第一道防线。

### 2.2 自动 Rekey 机制
V5 引入了密钥生命周期管理。当单个 WebTransport 会话写入记录数达到 **2^32** 次时，连接将自动断开。
- **客户端行为**：客户端会自动感知并建立新会话，过程对用户透明。
- **部署影响**：无须手动干预，仅需确保网关具备处理并发重连的能力。

---

## 3. 自动化 TLS 证书管理 (Caddy)

为了确保传输链路的极致安全，建议使用 Caddy 作为证书管理后端。

### 方案：共享证书卷

让 Caddy 负责证书的申领与自动续期，Gateway 通过共享卷读取证书文件。

**Caddyfile 配置示例:**
```caddy
your-domain.com {
    tls your-email@example.com
    # 仅作为证书来源，不进行协议层转发
}
```

---
 
 ## 4. 性能调优 (Kernel Tuning)
 
 为了在高延迟长距离链路中跑满带宽（BDP 适配），务必对 Linux 内核参数进行优化。
 
 ### 推荐 sysctl 配置
 在 `/etc/sysctl.conf` 中添加以下内容并执行 `sysctl -p`：
 
 ```bash
 # 开启 BBR 拥塞控制
 net.core.default_qdisc = fq
 net.ipv4.tcp_congestion_control = bbr
 
 # 增大 UDP 缓冲区上限 (关键优化 for QUIC)
 # 16MB 缓冲区足以支撑 1Gbps @ 200ms RTT
 net.core.rmem_max = 16777216
 net.core.wmem_max = 16777216
 net.core.rmem_default = 16777216
 net.core.wmem_default = 16777216
 ```
 
 > **注意**：如果不调整此参数，当下载速度超过 50MB/s 时可能会观察到丢包导致的吞吐量剧烈波动。
 
 ---
 
 ## 5. 云原生与边缘平台部署 (PaaS)

`aether-gateway` 已针对云原生环境进行了高度适配，支持在 Fly.io、Cloudflare Container 等容器平台上运行。

### 核心优化特性
1. **动态端口适配**：自动识别 `$PORT` 环境变量，无需手动映射内部端口。
2. **零配置自启动**：在缺失本地存储或证书的环境下，程序将自动生成临时安全链路以保障服务可用性。

### 部署要点
- **协议限制**：务必确认平台已放行 **UDP** 流量，否则 WebTransport 无法建立连接。
- **健康检查**：
  - **推荐路径**：`/` (根路径)。
  - **安全性说明**：由于网关内置主动探测防御，非协议请求访问根路径将返回“伪装站点”内容。使用 `/` 作为健康检查可以完美隐藏服务特征，避免暴露协议入口路径 `/v1/api/sync`。

---

## 6. 其它环境说明

项目依然保留了在 Cloudflare Workers 等边缘脚本环境运行的能力（源码参考 `src/worker.js`）。

> [!NOTE]
> **当前状态**：目前 Cloudflare Worker 尚未原生支持 WebTransport。虽然我们的代码已按照标准实现，但仍需等待 Cloudflare 平台开启该功能后即可直接投入使用。

在极高性能或低延迟要求场景下，强烈建议优先使用 Go 版本的 Gateway 以获得完整的性能优势与更强的防御特性。
