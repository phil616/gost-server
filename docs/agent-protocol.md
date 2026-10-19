# 管理 WSS 协议 v1

地址：`wss://<origin>/agent/v1/ws`。只接受 WebSocket，单条 JSON 消息最大 1 MiB。TLS 证书与域名必须验证。每条消息为 `{ "v": 1, "type": "...", "id": "UUID", "payload": {...} }`。

## 认证

1. 服务端发送 `challenge`，payload 为 `{ "nonce": "32-byte-random-base64url" }`。
2. 客户端在 30 秒内发送 `register` 或 `authenticate`。签名是 Ed25519 对 nonce 的 UTF-8 字节签名，base64url 无填充编码。
3. register 包含 request_id、token、公钥、签名、os、arch、version。公钥为 32 字节 Ed25519 公钥的 base64url。
4. authenticate 包含 client_id、签名；服务端使用已注册公钥。
5. 成功返回 `authenticated`，含 client_id、revision。失败返回 `error` 后关闭。

注册响应丢失时，用同 request_id、公钥和新挑战签名重试；无需再次消费 token。健康重复设备会话返回 `duplicate_instance`。

## 配置

客户端发送 `hello`，服务端在对应 Edge 授权策略确认后下发 `desired_config`。payload 是 Go `model.ClientConfig` 的 JSON，含 schema_version、client_id、revision、config_hash、tunnels。每条隧道包含完整 Tunnel 字段及 provider_credential（停用对象可省略）。

摘要算法：将 config_hash 置为空字符串，将 tunnels 按 id 升序排列，用 Go encoding/json 序列化定义的类型后计算 SHA-256 十六进制。需要其他语言客户端时必须保持字段顺序和相同序列化，或基于已发布编码建立兼容测试。

客户端回报 `config_ack`：received_revision、applied_revision、errors（TunnelID 到错误）以及 runtime 数组。applied_revision 仅在全部对象应用成功时推进。连接未建立不会阻止配置 applied，运行状态独立报告。

重复 revision 可重放，更旧版本忽略，同版本摘要冲突拒绝。客户端断线重连发送 hello，服务端重新同步所需完整快照。

## 状态

`runtime_event`、`runtime_snapshot`、`heartbeat` 携带 received_revision、applied_revision 和可选 runtime。客户端每 15 秒发快照，收到服务端 heartbeat 后保持会话。服务端 45 秒标 stale，60 秒清理失联会话；客户端 45 秒未收到服务端消息时重连。

运行状态包含 tunnel_id、generation、state、error、next_retry_at、retry_count、字节计数和 updated_at。管理连接、反向连接和目标健康分别判断。

网络重试计数独立于业务消息重发；数据连接不随管理 WSS 断开而关闭。业务配置只包含结构化转发参数，不执行 shell 或接受原始任意 GOST 配置。

## 消息索引

| 方向 | type | payload / 作用 |
| --- | --- | --- |
| Control → 设备 | challenge | nonce，当前连接的随机挑战 |
| 设备 → Control | register | request_id、token、public_key、signature、os、arch、version |
| 设备 → Control | authenticate | client_id、signature；签名本连接 nonce |
| Control → 设备 | authenticated | client_id、revision；表示身份验证完成 |
| 设备 → Control | hello | received_revision、applied_revision、runtime |
| Control → 设备 | desired_config | 完整 ClientConfig，含 hash 与每条隧道 Provider |
| 设备 → Control | config_ack | received/applied、errors、runtime |
| 双向 | heartbeat | 服务端时间或客户端当前确认/运行状态 |
| 设备 → Control | runtime_event | 单次运行变化后的状态集合，不推进持久确认版本 |
| 设备 → Control | runtime_snapshot | 定期完整状态，更新最近联系与确认信息 |
| Control → 设备 | error | authentication_failed、duplicate_instance 等错误后关闭 |

管理 JSON 的 id 是消息标识，不是 REST 的 Idempotency-Key。协议不提供任意 RPC、shell 命令或用户原始 GOST 配置上传。版本不匹配、非法 ACK 或无效 JSON 可以导致连接关闭，不承诺每种错误均有可恢复 error 消息。

## 配置与 ACK 示例

完整字段定义以 `internal/model/model.go` 为准，以下 hash 和标识为示意值：

```json
{
  "v": 1,
  "type": "config_ack",
  "id": "MESSAGE_ID",
  "payload": {
    "received_revision": 8,
    "applied_revision": 8,
    "errors": {},
    "runtime": [
      {
        "tunnel_id": "TUNNEL_ID",
        "generation": 2,
        "state": "connecting",
        "retry_count": 0,
        "input_bytes": 0,
        "output_bytes": 0,
        "updated_at": "2026-10-18T00:00:00Z"
      }
    ]
  }
}
```

applied=8 且状态 connecting 是合法组合：运行对象已经应用，但实际 Bind 尚未成功。不能把 ACK 转换为业务在线结论。

## 重连与重复会话

同一 ClientID 同时出现健康管理会话时，新会话收到 duplicate_instance。旧会话达到失联阈值后才能被替换。设备必须独立持久化管理退避，不以重新发送 register 绕过身份限制。

Control 重启会断开管理 WS，但不强制关闭客户端数据 Runtime；设备重连后重新 hello，Control 在策略确认后重发所需配置。新消息的运行 generation 与快照时间共同用于避免旧对象状态覆盖新对象。

跨语言管理客户端需要复现 Go JSON 摘要规则并验证 Ed25519/base64url 编码，不能只根据字段名称对原始 JSON 做字符串排序。协议改动需要同步 [数据模型](data-model.md)、[技术实现](implementation.md) 和兼容测试。
