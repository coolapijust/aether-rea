# Aether-Realist

Aether-Realist 是一套运行于 WebTransport (HTTP/3) 之上的无状态、分段式、可配置边缘中转协议。该仓库包含协议规范、Cloudflare Worker 参考实现、Go 客户端与 GUI 配置台。需要等待Cloudflare Worker原生支持WebTransport 才可使用。

## 目录结构

- `docs/aether-realist-protocol.md`：协议规范（Record framing / Metadata / Error）。
- `src/worker.js`：Cloudflare Worker 参考实现。
- `cmd/aether-client`：Go WebTransport + SOCKS5 客户端。
- `ui/`：现代化 GUI 配置台（静态前端）。
- `docs/deployment.md`：部署与密钥配置说明。

## 架构概览

1. **WebTransport 会话**：客户端通过 `/v1/api/sync` 建立 WebTransport session。
2. **Metadata Record**：首包描述目标地址、端口与选项（AES-GCM + HKDF）。
3. **Record Switcher**：Worker 仅转发数据，严格遵循背压。
4. **Session Rotation**：客户端定期轮换 session，降低流量画像特征。

## GUI 配置台

`ui/` 提供一个轻量控制台，可完成：

- 生成客户端启动命令。
- 读取 https://ip.v2too.top/ 并展示优选 IP。
- 提示 `--dial-addr` 的最佳接入方式。

桌面端可以使用 `cmd/aether-studio`，该程序将 Client Studio 的能力移植进本地 GUI，并补充系统代理开关与托盘管理入口。

```bash
go build -o aether-studio ./cmd/aether-studio
```

## 部署方法

### 1. Worker 部署

参考 `docs/deployment.md`：

```bash
wrangler secret put PSK
wrangler deploy
```

### 3. Gateway 服务端 (New)

Aether-Realist 现在支持独立的 Go 服务端 `aether-gateway`，支持 Docker 部署。

#### Docker 部署 (推荐)

从 GitHub Container Registry 拉取镜像：

```bash
docker pull ghcr.io/coolapijust/aether-rea:latest
```

启动容器：
```bash
docker run -d \
  --name aether-gateway \
  -p 4433:4433/udp \
  -p 4433:4433/tcp \
  -v /path/to/certs:/certs \
  ghcr.io/coolapijust/aether-rea:latest \
  -cert /certs/fullchain.pem \
  -key /certs/privkey.pem \
  -psk "your-secret-password"
```

#### 云平台部署 (ClawCloud / Cloud Run)

由于支持 `$PORT` 环境变量和自动自签名证书，本服务可直接部署于容器托管平台。详情请参考 [deployment.md](docs/deployment.md#4-云平台部署-clawcloud--cloud-run--paas)。

#### 手动编译

```bash
go build -o aether-gateway ./cmd/aether-gateway
./aether-gateway -cert cert.pem -key key.pem -psk "secret"
```

## 客户端构建

```bash
go build -o aether-client ./cmd/aether-client
```

### 启动示例

```bash
# 连接到自建 Gateway
./aether-client \
  --url https://your-gateway-ip:4433/v1/api/sync \
  --psk "your-secret-password" \
  --rotate 20m
```

## GitHub Actions

- `.github/workflows/build-gateway.yml`：自动构建 `aether-gateway` 二进制文件（Windows/Linux）并发布 Docker 镜像到 GHCR。
- `.github/workflows/build-client.yml`：自动编译客户端 Windows `.exe` 版本。
