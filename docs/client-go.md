# Go 客户端（参考实现）

该客户端以 SOCKS5 形式提供入站，并通过 WebTransport 将流量封装为 Aether-Realist 记录。

## 构建

```bash
go build -o aether-client ./cmd/aether-client
```

## 运行

```bash
./aether-client \
  --url https://your-domain.com/v1/api/sync \
  --psk "$PSK" \
  --listen 127.0.0.1:1080 \
  --rotate 20m \
  --max-padding 128 \
  --skip-verify # 如果服务端使用自签名证书，请务必加上此参数
```

## Anycast 优选 IP

如果需要将域名映射到优选 IP，可使用两种方式：

1. **系统 hosts 覆盖**：将 `your-domain.com` 映射到优选 IP（UDP 443 必须可达）。
2. **客户端覆盖**：使用 `--dial-addr` 强制 QUIC 直连指定 IP：

```bash
./aether-client \
  --url https://your-domain.com/v1/api/sync \
  --psk "$PSK" \
  --dial-addr 203.0.113.10:443
```

> 注意：`--dial-addr` 仅改变 QUIC 连接地址，TLS SNI 仍使用 URL 中的域名。

## 自动优选

客户端支持通过 `--auto-ip` 从 https://ip.v2too.top/ 拉取候选 IP，并测试 TCP 443 延迟后选择最优地址：

```bash
./aether-client \
  --url https://your-domain.com/v1/api/sync \
  --psk "$PSK" \
  --auto-ip
```

## Session Rotation

`--rotate` 定时器会触发会话轮换：

- 客户端会关闭当前 WebTransport Session。
- 后续请求将自动建立新 Session。

这有助于对抗长连接的统计特征。
