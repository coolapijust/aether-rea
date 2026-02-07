# 部署与环境配置

## 1. Aether Gateway (Docker - 推荐)

Aether-Realist 提供了独立的 Docker 镜像，支持 HTTP/3 WebTransport。

### 启动命令

> [!TIP]
> **自动 TLS**：如果未提供 `-cert` 和 `-key` 参数，服务端将自动生成内存中的 **自签名证书**。这非常适合测试环境或处于反向代理（如 Caddy）后端的情况。

```bash
# 方式 A：使用自签名证书（快速测试）
docker run -d \
  --name aether-gateway \
  -p 4433:4433/udp \
  -p 4433:4433/tcp \
  ghcr.io/coolapijust/aether-rea:latest \
  -psk "your-strong-password"

# 方式 B：使用自有证书（生产推荐）
docker run -d \
  --name aether-gateway \
  -v /path/to/certs:/certs \
  -p 4433:4433/udp \
  -p 4433:4433/tcp \
  ghcr.io/coolapijust/aether-rea:latest \
  -cert /certs/fullchain.pem \
  -key /certs/privkey.pem \
  -psk "your-strong-password"
```

### 必需参数
- `-cert`: TLS 证书文件路径。
- `-key`: TLS 私钥文件路径。
- `-psk`: 预共享密钥，客户端连接时需一致。

---

## 2. Cloudflare Worker 配置 (Legacy)

### Wrangler 配置

在项目根目录添加 `wrangler.toml`，并启用 `nodejs_compat`：

```toml
name = "aether-realist"
main = "src/worker.js"
compatibility_date = "2024-02-06"
nodejs_compat = true

[vars]
SECRET_PATH = "/v1/api/sync"
```

- `SECRET_PATH` 用于控制 WebTransport 入口路径。
- 根路径 `/` 将返回静态页面以满足伪装需求。

### Secret 管理

`PSK` 必须通过 Wrangler secret 设置：

```bash
wrangler secret put PSK
```

部署时请确保环境变量已经生效。
