# Aether-Realist 协议定义（V4）

> 状态：正式版本 (Finalized)

## 1. 术语与约定

- **Record**：协议最小封装单元。
- **Metadata Record**：每个双向流首条记录，用于描述目标地址与选项。
- **Data Record**：载荷数据记录。
- **Error Record**：服务端或客户端的错误返回记录。
- **网络字节序**：所有多字节整数均为 Big Endian。

## 2. 传输与会话

- **承载层**：WebTransport over HTTP/3。
- **无状态**：服务端仅在单个 WebTransport 会话范围内维护状态，关闭即释放。
- **会话握手**：依赖 HTTP/3 + WebTransport 建立，不引入额外握手包。
- **0-RTT 支持**：V4 版本 **开启 0-RTT** (Early Data)。为了防御重放攻击，协议内置了时间戳校验与 IV 去重缓存（ReplayCache）。

## 2.1 握手状态机 (Handshake State Machine)

为了消除实现歧义并防止互等死锁，双方必须严格遵循以下状态机流转：

1.  **Client**: `OpenStream()` -> **立即发送** `Metadata Record` -> 等待 `Data Record` 或直接发送 `Data Record`。
    *   *Client 不得等待 Server 的任何初始响应即可发送后续数据。*
2.  **Server**: `AcceptStream()` -> **阻塞读取** `Metadata Record` -> 解析目标与路由 -> 建立连接/转发。
    *   *Server 在收到完整 Metadata 前不得发送任何数据。如果验证失败，服务端应静默关闭连接。*

## 3. Record 帧结构

每条 Record 采用统一的封装格式，V4 协议头长度为 **30 字节**：

```
0                   1                   2                   3
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+---------------------------------------------------------------+
|                     Length Prefix (u32)                       |
+---------------------------------------------------------------+
| Version(u8) |  Type (u8) |                                    |
+-------------+------------+                                    +
|                     Timestamp Nano (u64)                      |
+                          +------------------------------------+
|                          |        Payload Length (u32)        |
+--------------------------+------------------------------------+
|                     Padding Length (u32)                      |
+---------------------------------------------------------------+
|                        Nonce/IV (12B)                         |
+---------------------------------------------------------------+
|                         Payload (var)                         |
+---------------------------------------------------------------+
|                         Padding (var)                         |
+---------------------------------------------------------------+
```

- **Length Prefix**：Record 的总长度（Header + Payload + Padding），不包含自身长度字段。
- **Version**：协议版本，V4 必须为 `0x04`。
- **Type**：Record 类型。
- **Timestamp Nano**：发送端纳秒时间戳，用于防御重放。
- **Payload Length**：载荷长度。
- **Padding Length**：填充长度。
- **Nonce/IV**：12 字节随机值，用于 AEAD。

### 3.1 Record 类型

| Type | 名称             | 说明 |
| ---- | ---------------- | ---- |
| 0x01 | Metadata Record  | 首条记录，携带目标信息 + 选项（加密）。 |
| 0x02 | Data Record      | 数据记录。 |
| 0x03 | Ping Record      | 心跳探测。 |
| 0x04 | Pong Record      | 心跳响应。 |
| 0x7F | Error Record     | 错误返回。 |

## 4. Metadata Record

### 4.2 加密

Metadata Record 的 Payload 必须使用 `AES-128-GCM` 加密。

- **Key Derivation (Zero-Sync V4)**：
    - `PRK = HKDF-Extract(salt=IV, IKM=PSK)`。
    - `Key = HKDF-Expand(PRK, info="aether-realist-v4", L=16)`。
- **Nonce/IV**：Record Header 中的 12 字节随机值（IV）。
- **AAD**：完整的 Record Header（30 字节）。通过将 Version 和 Timestamp 纳入 AAD，确保了协议头字段不可篡改。

## 5. 防重放机制 (Anti-Replay)

服务端必须执行双重校验：
1.  **时间窗口校验**：`|ServerTime - RecordTimestamp| < 30s`。
2.  **IV 去重校验**：在 ReplayCache 中记录已收到的 IV，若 IV 重复则判定为重放攻击并丢弃。

## 6. 流量混淆 (Traffic Chunking)

- 发送端应将数据流拆分为 **2KB - 16KB** 的随机大小片段。
- 禁止基于 MTU 的对齐，以对抗统计学指纹分析。

## 7. 错误处理与主动防御

- **静默丢弃 (Silent Drop)**：对于非法版本、协议格式错误或认证失败的 Metadata 握手，服务端应直接关闭 Stream 引发“静默失败”，不得返回明文错误记录，以防止主动探测特征泄露。
- **Error Record**：仅用于已建立连接后的业务逻辑错误反馈。
