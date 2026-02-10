# 生产部署指南

本文档详细说明了 Aether-Realist V5 版本的生产级部署方案。

---

## 0. 快速启动 (推荐)

直接运行以下命令，脚本将自动下载依赖并完成配置：

```bash
curl -sL "https://raw.githubusercontent.com/coolapijust/Aether-Realist/main/deploy.sh?$(date +%s)" -o deploy.sh && chmod +x deploy.sh && ./deploy.sh
```

> [!TIP]
> 脚本会引导你输入 `DOMAIN` (域名) 和 `PSK` (预共享密钥)。如果您处于中国大陆环境，请确保您的服务器可以正常访问 GitHub Raw 服务。

---

## 1. 核心镜像部署 (Docker)

## 1. 核心镜像部署 (Docker)

Aether-Realist 提供了高性能的 Docker 镜像，内置 HTTP/3 WebTransport 支持与内存优化机制。V5 版本引入了更强的抗重放机制与 Session 自动轮换。

### 生产推荐：Docker Compose 编排 (KISS 架构)

V5 版本推荐直接面向网络，移除冗余的代理层以获得极致性能。

```yaml
services:
  # 核心服务端 (Host 直连 + 多态伪装)
  # 自动处理 443/8080 端口与 TLS 握手
  aether-gateway-core:
    image: ghcr.io/coolapijust/aether-realist:main
    container_name: aether-gateway-core
    restart: always
    network_mode: host
    environment:
      - PSK=${PSK}
      - LISTEN_ADDR=:${CADDY_PORT} # 自动探测的端口
      - SSL_CERT_FILE=/certs/server.crt
      - SSL_KEY_FILE=/certs/server.key
      - DECOY_ROOT=/decoy
    volumes:
      - ./certs:/certs:ro
      - ${DECOY_PATH}:/decoy:ro # 动态挂载伪装目录
    cap_add:
      - NET_ADMIN
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

---

## 3. 自动化 TLS 证书管理 (外部协作)

为了确保传输链路的极致安全与高性能，建议在宿主机使用专门的 ACME 工具管理证书。

### 方案：配合 acme.sh

Backend 支持通过 `SIGHUP` 信号热重载证书。您可以配置 `acme.sh` 在更新证书后通知容器。

**自动化流程:**
1. **申请证书**:
```bash
acme.sh --issue -d your-domain.com --standalone
```

2. **自动同步与重载**:
将以下命令加入您的 `acme.sh` 安装指令中：
```bash
acme.sh --install-cert -d your-domain.com \
    --cert-file      ./deploy/certs/server.crt \
    --key-file       ./deploy/certs/server.key \
    --reloadcmd      "docker kill -s HUP aether-gateway-core"
```

此方案下，Backend 实时读取磁盘证书，续签过程对业务完全透明。

---

## 4. 容器时间同步故障排查

容器时间与宿主机不同步会导致时间戳验证失败，影响连接建立。

### 自动检测

部署脚本会在启动后自动检查容器时间偏差：

```bash
./deploy.sh status
```

如果偏差超过 5 秒，会显示警告并提供修复建议。

### 手动修复

**方案 1：同步宿主机时间**
```bash
# 安装 ntpdate（如果未安装）
sudo apt-get install ntpdate  # Debian/Ubuntu
sudo yum install ntpdate       # CentOS/RHEL

# 同步时间
sudo ntpdate -u pool.ntp.org

# 重启容器使时间生效
docker restart aether-gateway-core
```

**方案 2：检查时区配置**
```bash
# 查看宿主机时区
timedatectl

# 查看容器时区
docker exec aether-gateway-core date

# 如果时区不一致，重新部署容器（会自动挂载宿主机时区）
docker compose -f deploy/docker-compose.yml up -d --force-recreate
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
1. **端口动态自愈**：脚本会自动检测 443 端口占用情况。若被 Nginx 占用，将自动引导至 8080 端口或用户指定端口，并自动配置 Backend 监听。
2. **多态深度伪装 (Decoy Engine)**：
    - **模板选择**：内置“流媒体诊断”、“云协作平台”等高逼真专业模板。
    - **自定义接入**：支持 Git Clone 任意仓库或挂载本地目录作为伪装站。
    - **所有权混淆**：自动随机化站点元数据（Title/CSS/Color），防止指纹被扫描识别。

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
