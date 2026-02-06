# 部署与环境配置（Cloudflare）

## Wrangler 配置

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

## Secret 管理

`PSK` 必须通过 Wrangler secret 设置：

```bash
wrangler secret put PSK
```

部署时请确保环境变量已经生效。
