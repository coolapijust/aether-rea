# Aether-Realist 协议定义（V3）

> 状态：草案（Draft）

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

## 3. Record 帧结构

每条 Record 采用统一的封装格式：

```
0                   1                   2                   3
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+---------------------------------------------------------------+
|                     Length Prefix (u32)                       |
+---------------------------------------------------------------+
|  Type (u8)  | Flags (u8) |         Reserved (u16)             |
+---------------------------------------------------------------+
|                     Payload Length (u32)                      |
+---------------------------------------------------------------+
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
- **Type**：Record 类型。
- **Flags**：扩展标志位，未使用位必须为 0。
- **Reserved**：保留字段，必须为 0。
- **Payload Length**：载荷长度。
- **Padding Length**：填充长度。
- **Nonce/IV**：12 字节随机值，用于 AEAD 或保留全 0。

### 3.1 Record 类型

| Type | 名称             | 说明 |
| ---- | ---------------- | ---- |
| 0x01 | Metadata Record  | 首条记录，携带目标信息 + 选项（加密）。 |
| 0x02 | Data Record      | 数据记录。 |
| 0x7F | Error Record     | 错误返回。 |

## 4. Metadata Record

### 4.1 明文结构（加密前）

```
0                   1                   2                   3
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+---------------------------------------------------------------+
|   Address Type (u8)   |              Port (u16)               |
+---------------------------------------------------------------+
|                    Target Address (var)                       |
+---------------------------------------------------------------+
|                    Options Length (u16)                       |
+---------------------------------------------------------------+
|                         Options (var)                         |
+---------------------------------------------------------------+
```

- **Address Type**：0x01=IPv4，0x02=IPv6，0x03=Domain。
- **Port**：目标端口。
- **Target Address**：按 Address Type 编码。
- **Options**：TLV 结构，客户端自定义。

### 4.2 加密

Metadata Record 的 Payload 必须使用 `AES-128-GCM` 加密。

- **PSK**：预共享密钥，由部署者配置。
- **Key Derivation**：`HKDF-SHA256(PSK, salt="aether-realist-v3", info=stream-id)`，截断前 16 字节作为密钥。
- **Nonce/IV**：Record Header 中的 12 字节随机值。
- **AAD**：Record Header（Type/Flags/Reserved/Payload Length/Padding Length/IV）。

## 5. Data Record

- Payload 为原始数据片段。
- Padding 为随机填充（可为 0）。
- 服务端必须保持 **流水线转发**：
  - 入站：WebTransport Data Record → Socket
  - 出站：Socket → WebTransport Data Record
- 应使用流式写入确保背压生效（`readable.pipeTo(writable)` 或等价实现）。

## 6. 会话与流终止

- 任一方向出现 FIN 或异常关闭时，必须立即释放对端 Socket/Stream。
- 服务端不得复用已关闭的连接上下文。

## 7. 错误处理

### 7.1 Error Record Payload

```
0                   1                   2                   3
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+---------------------------------------------------------------+
|               Error Code (u16)         |  Reserved (u16)      |
+---------------------------------------------------------------+
|                      Error Message (utf-8)                    |
+---------------------------------------------------------------+
```

### 7.2 标准错误码

| Code | 名称                 | 说明 |
| ---- | -------------------- | ---- |
| 0x0001 | ERR_BAD_RECORD      | Record 格式错误。 |
| 0x0002 | ERR_METADATA_DECRYPT| Metadata 解密失败。 |
| 0x0003 | ERR_UNSUPPORTED     | 不支持的类型或选项。 |
| 0x0004 | ERR_TARGET_CONNECT  | 目标连接失败。 |
| 0x0005 | ERR_STREAM_ABORT    | 流异常终止。 |
| 0x0006 | ERR_RESOURCE_LIMIT  | 超限（并发或流量）。 |
| 0x0007 | ERR_TIMEOUT         | 超时。 |

## 8. 客户端可配置项

- **Multiplexing Strategy**：流并发上限（通过客户端配置控制）。
- **Chunking & Padding**：Payload 与 Padding 的长度策略。
- **Persistence Policy**：会话轮换周期。
- **Crypto Profile**：分段加解密开关与 IV 派生策略。

## 9. sing-box 语义映射

| Aether-Realist 概念 | sing-box 配置映射 | 说明 |
| --- | --- | --- |
| WebTransport Session | `transport: { type: "webtransport" }` | 物理承载层 |
| Bidirectional Stream | `multiplex: { enabled: true }` | 逻辑并发子流 |
| Metadata Record | `outbound: { address, port }` | 目标握手信息 |
| Data Record | `packet_encoding: "xhttp"` | 分段与混淆 |
| Session Rotation | `connection_reuse: false` | 长连接对冲 |
