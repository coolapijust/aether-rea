# Aether-Realist 设计文档 (架构、安全与性能)

本文档深入介绍 Aether-Realist V5 版本的核心设计哲学、系统架构以及在安全与性能方面的优化实践。

---

## 1. 核心架构 (Architecture)

Aether-Realist 采用 **Core-Daemon-GUI** 分离的设计架构，确保了核心逻辑的稳定性与前端界面的高度解耦。

### 1.1 系统组件
- **Aether Core (internal/core)**: 协议的核心实现，负责 WebTransport 会话管理、分段混淆、流式背压控制以及规则匹配。
- **Aether Daemon (aetherd)**: 基于 Core 开发的后台进程，提供 SOCKS5 代理接口和 WebSocket 管理接口。
- **Tauri GUI (gui/)**: 基于 React + TypeScript 的现代化界面。

---

## 2. 安全机制 (Security)

安全是 Aether-Realist 的生命线。V5 版本在 V4 基础上进一步强化了协议鲁棒性和密码学安全性。

### 2.1 主动探测防御 (Active Probe Defense)
在生产部署中，利用 Caddy/OpenResty 的匹配能力实现"主动拒绝"：
- **指纹匹配**: 只有携带特定协议特征（如 `Upgrade: webtransport`）的请求才会被转发。
- **陷阱响应**: 非法探测将收到诱导性的 401 JSON 错误，增加攻击者的测向难度。

### 2.2 深度伪装 (Decoy System)
通过 Caddy 的回落机制：
- **默认站点**: 任何不满足 Aether 协议特征的流量都会被路由到一个极其"正经"的 **Decoy Site**。

### 2.3 协议层安全 (V5 增强)
- **元数据加密**: 采用 `AES-128-GCM` 对目标地址（Metadata）进行强加密，**认证标签强制 16 字节**。
- **V5 Nonce 机制**: 
    - **SessionID (4B) + Counter (8B)** 取代 V4 的随机 IV，彻底消除 nonce 复用风险。
    - HKDF 使用 **SessionID** 作为 salt 派生密钥，而非随机 IV。
- **协议头加固 (30 Bytes)**:
    - V5 头部包含 `Version`、`Timestamp`、`SessionID` 和 `Counter` 字段，全部纳入 AEAD 的 **AAD (附加认证数据)**。
    - **设计优势**: 攻击者无法在不破坏加密验证的情况下篡改任何头部字段。
- **抗重放攻击 (Anti-Replay)**:
    - **时间戳校验**: 服务端强制 Record 时间戳与本地时间偏差在 ±30s 内。
    - **计数器单调性检查**: V5 采用严格的 **Counter 递增检查**，替代 V4 的 IV 去重缓存，提供确定性的抗重放保证。
- **密钥生命周期管理**: 单个会话 Counter 达到 **2^32** 后强制重新建立会话（rekey），符合 GCM 安全边界要求。
- **隐式特征消除**: 协议头在 TLS 内部传输，且字段尽可能随机化或对齐。
- **静默丢弃 (Silent Drop)**: 针对探测行为，服务端直接关闭流，不产生合规的错误响应，使特征完全消失。
- **流量混淆 (Traffic Obfuscation)**: 在应用层引入 **2KB-16KB 随机化分块**。

---

## 3. 性能优化 (Performance)

### 3.1 HTTP/3 (QUIC) 优势
- **0-RTT 建连**: V5 配合计数器机制，**正式开启 0-RTT**，实现真正的高性能握手恢复。
- **无队头阻塞**: 单个流丢包不影响其他并发连接。
- **连接迁移**: 在移动端场景保持 SOCKS5 连接不中断。
- **自愈能力 (Self-Healing)**: Core 内置心跳监测与透明重连机制。

### 3.2 吞吐量极致优化
针对高延迟链路 (High BDP Links)，实施了激进的参数调优：
- **超大流控窗口**: `MaxConnectionReceiveWindow` 设为 **48MB**，`MaxStreamReceiveWindow` 设为 **32MB**。
- **加速爬升 (Fast Ramp-up)**: 初始窗口 (Initial Window) 提升至 **4MB**。
- **零损耗 I/O**: 大块传输缓冲区 (512KB) 显著降低 CPU 上下文切换。
