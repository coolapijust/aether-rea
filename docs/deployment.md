# 生产部署指南

本文档详细说明了 Aether-Realist V5.1 版本的生产级部署方案。

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

Aether-Realist 提供了高性能的 Docker 镜像，内置 HTTP/3 WebTransport 支持与内存优化机制。V5.1 版本引入了更强的抗重放机制、Session 自动轮换以及分级流控窗口。

### 生产推荐：Docker Compose 编排 (KISS 架构)

V5.1 版本推荐直接面向网络，移除冗余的代理层以获得极致性能。

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
      - WINDOW_PROFILE=normal # 可选: conservative, normal, aggressive
    volumes:
      - ./certs:/certs:ro
      - ${DECOY_PATH:-deploy/decoy}:/decoy:ro # 动态挂载伪装目录 (默认为 deploy/decoy)
    cap_add:
      - NET_ADMIN
```

### 运行时参数说明
- `-psk`: **必须**。所有客户端连接必须匹配此密钥。
- `-cert / -key`: 证书与私钥路径。若未提供，网关将自动降级为自签名证书模式，适用于测试或负载均衡器后端。
- `-listen`: 监听地址，默认为 `:8080`。

---

## 2. 安全与性能加固 (V5.1 特性)

### 2.1 抗重放与 0-RTT
V5.1 协议默认开启 **0-RTT** 支持。为了确保安全性，服务端实现了严格的 **单调计数器（Monotonic Counter）** 校验。
- **运维建议**：确保服务器系统时间同步（使用 NTP），虽然 V5.1 主要依赖计数器防止重放，但 30s 的时间窗口校验依然作为第一道防线。

### 2.2 自动 Rekey 机制
V5.1 引入了密钥生命周期管理。当单个 WebTransport 会话写入记录数达到 **2^32** 次时，连接将自动断开。
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

## 5. 客户端连接故障排查 (Silent Connectivity)

如果客户端无法连接且服务端日志无报错，通常是 **UDP 端口被阻断** 或 **证书不信任**。

### 1. 检查 UDP 端口与防火墙

运行状态检查命令：
```bash
./deploy.sh status
```

**关注输出中的以下部分：**
```
--- 网络与防火墙检查 (QUIC/HTTP3) ---
[OK] UDP 端口 443 监听正常
[WARN] UFW 防火墙已开启。请确保放行 UDP 端口：
      sudo ufw allow 443/udp (WebTransport 核心)
      sudo ufw allow 443/tcp (HTTPS 访问)
      sudo ufw allow 80/tcp (HTTP 跳转)
```
**注意**：很多云厂商（AWS/GCP/Aliyun）的安全组默认只放行 TCP。**必须显式放行 UDP 443 以及 TCP 80/443**。

### 2. 客户端证书验证

如果您使用的是**自签名证书**（默认生成），客户端必须跳过证书验证才能连接。

- **Desktop GUI**: 勾选 `Allow Insecure` 或 `Skip Verify`。
- **CLI**: 增加 `--insecure` 参数。


---

## 6. VPS 选型与深度性能调优 (V5.1 优化)

为了在高延迟长距离链路中跑满带宽（BDP 适配），并确保 WebTransport 协议的稳定性，建议参考以下配置与优化方案。

### 4.1 硬件配置推荐

| 类型 | 场景 | CPU | 内存 | 带宽 |
| :--- | :--- | :--- | :--- | :--- |
| **基础型** | 个人日常 / 网页浏览 | 1 vCPU (AMD/Intel) | 512MB+ | 100Mbps+ |
| **进阶型** | 4K 串流 / 小团队 | 2 vCPU | 2GB+ | 500Mbps+ (CN2 GIA/9929) |
| **性能型** | 高并发 / 极低延迟 | 4 vCPU+ | 4GB+ | 1Gbps+ 独享 |

> **内存警告**：建议至少 1GB 内存。512MB 机器在高并发下可能因 OOM (Out of Memory) 导致断流。

### 4.2 操作系统选择
*   **首选**: Debian 11 / 12 (内核较新，对 QUIC 支持佳，系统占用低)
*   **次选**: Ubuntu 22.04 LTS
*   **不推荐**: CentOS 7 (内核过旧，严重制约 UDP/QUIC 性能)

### 4.3 关键系统优化 (sysctl)
Aether V5.1 基于 UDP 协议，必须优化 Linux 内核参数以获得最佳吞吐量。请在 `/etc/sysctl.conf` 中添加：

```bash
# 开启 BBR 拥塞控制 (显著改善丢包环境速度)
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr

# 增大 UDP 缓冲区上限 (解决 QUIC 吞吐瓶颈)
# 建议设置 16MB - 32MB 以支撑 1Gbps+
net.core.rmem_max = 33554432
net.core.wmem_max = 33554432
net.core.rmem_default = 8388608
net.core.wmem_default = 8388608

# 优化 UDP 发包效率
net.ipv4.udp_rmem_min = 16384
net.ipv4.udp_wmem_min = 16384
```
应用配置：`sysctl -p`

### 4.4 文件描述符与并发限制
高并发场景下，默认的文件描述符限制（通常为 1024）可能成为瓶颈。

编辑 `/etc/security/limits.conf`，添加：
```text
* soft nofile 51200
* hard nofile 51200
root soft nofile 51200
root hard nofile 51200
```
*需重启服务器生效*。
 
 ---

## 7. 云原生与分级窗口 (WINDOW_PROFILE)

`aether-gateway` 已针对云原生环境进行了高度适配，支持在 Fly.io、Cloudflare Container 等容器平台上运行。

### 7.1 分级窗口逻辑
V5.1 引入了分级流控窗口配置，通过环境变量 `WINDOW_PROFILE` 设置，以平衡性能与指纹特征：
- **`conservative`**: 512KB 窗口，隐蔽性极高，适用于对抗深度审查。
- **`normal`** (默认): 2MB 窗口，通用首选。
- **`aggressive`**: 4MB 窗口，适用于高带宽长距离链路，但在指纹特征上更明显。

### 核心优化特性
1. **端口动态自愈**：脚本会自动检测 443 端口占用情况。若被 Nginx 占用，将自动引导至 8080 端口或用户指定端口，并自动配置 Backend 监听。
2. **多态深度伪装 (Decoy Engine)**：
    - **模板选择**：内置“流媒体诊断”、“云协作平台”等高逼真专业模板。
    - **自定义接入**：支持 Git Clone 任意仓库或挂载本地目录作为伪装站。
    - **所有权混淆**：自动随机化站点元数据（Title/CSS/Color），防止指纹被扫描识别。

### 部署要点
- **协议限制**：务必确认平台已放行 **UDP** 流量，否则 WebTransport 无法建立连接。
- **健康检查**：
  - **安全性说明**：由于网关内置主动探测防御，非协议请求访问根路径将返回“伪装站点”内容。使用 `/` 作为健康检查可以完美隐藏服务特征，避免暴露协议入口路径 `/v1/api/sync`。

### 7.2 持久化与升级策略
- **伪装站保护**：`deploy.sh` 脚本在更新时会自动检测已存在的伪装站点（位于 `deploy/decoy`），并询问是否覆盖，防止误删您的自定义修改。
- **卸载保留**：执行卸载操作时，默认保留伪装站点及证书数据，方便快速重建或迁移。

---

## 8. 其它环境说明

项目依然保留了在 Cloudflare Workers 等边缘脚本环境运行的能力（源码参考 `src/worker.js`）。

> [!NOTE]
> **当前状态**：目前 Cloudflare Worker 尚未原生支持 WebTransport。虽然我们的代码已按照标准实现，但仍需等待 Cloudflare 平台开启该功能后即可直接投入使用。

在极高性能或低延迟要求场景下，强烈建议优先使用 Go 版本的 Gateway 以获得完整的性能优势与更强的防御特性。
