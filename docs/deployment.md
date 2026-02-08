# 生产部署指南

本指南详细说明了 Aether-Realist Gateway 的生产级部署方案，重点关注高可用性、安全防御与自动化 TLS 管理。

---

## 1. 核心镜像部署 (Docker)

Aether-Realist 提供了高性能的 Docker 镜像，内置 HTTP/3 WebTransport 支持与内存优化机制。

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

## 2. 自动化 TLS 证书管理 (Caddy)

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

## 3. 云原生与边缘平台部署 (PaaS)

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

## 4. 其它环境说明

项目依然保留了在 Cloudflare Workers 等边缘脚本环境运行的能力（源码参考 `src/worker.js`）。

> [!NOTE]
> **当前状态**：目前 Cloudflare Worker 尚未原生支持 WebTransport。虽然我们的代码已按照标准实现，但仍需等待 Cloudflare 平台开启该功能后即可直接投入使用。

在极高性能或低延迟要求场景下，强烈建议优先使用 Go 版本的 Gateway 以获得完整的性能优势与更强的防御特性。
