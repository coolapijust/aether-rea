# Aether-Realist

Aether-Realist 是一套运行于 WebTransport (HTTP/3) 之上的无状态、分段式、可配置边缘中转协议。该仓库包含协议规范、Cloudflare Worker 参考实现、Go 客户端与 GUI 配置台。

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

## 部署方法

### 1. Worker 部署

参考 `docs/deployment.md`：

```bash
wrangler secret put PSK
wrangler deploy
```

### 2. 客户端构建

```bash
go build -o aether-client ./cmd/aether-client
```

### 3. 启动示例

```bash
./aether-client \
  --url https://your-domain.com/v1/api/sync \
  --psk "$PSK" \
  --auto-ip \
  --rotate 20m
```

## GitHub Actions

`.github/workflows/build-client.yml` 会自动编译 Windows `.exe` 版本并作为 Artifact 输出。
