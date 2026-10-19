# 项目概览

## 目标与术语

平台将已有 TCP/UDP 服务通过唯一 Edge 提供给持有凭据的访问方。管理员统一管理设备与隧道，设备仅主动建立出站 WSS。GOST 负责协议与转发，平台负责身份、授权、固定目标、配置同步和状态。

| 名称 | 含义 | 标识 |
| --- | --- | --- |
| 管理员 | 登录 Web 管理资源 | Admin ID |
| Control | 持久化期望状态并协调 Edge 和设备 | revision、operation |
| Edge | 唯一数据入口和授权执行者 | policy revision、epoch |
| Client / 设备 | 运行 altascitunnel 的受管主机 | ClientID、Ed25519 身份 |
| Provider | 客户端为指定隧道执行反向 Bind 的角色 | 设备、TunnelID、generation |
| Visitor | 使用官方 gost 访问服务的外部进程 | 单隧道访问凭据 |
| Tunnel | 固定协议与目标的逻辑资源 | 创建时分配的 TunnelID |

隧道定义是 `(TunnelID, protocol, target_ip, target_port)`。`tcp,127.0.0.1,22` 表示客户端连接自己的 SSH 服务，不是客户端或 Edge 监听新业务端口。连接建立方向是主动出站，承载的业务数据双向传输。

## 完整流程

1. 管理员创建有效期 10 分钟的一次性注册 token。
2. 客户端启动等待注册，CLI 经本地 IPC 提交 token；设备持久化私钥与 request_id，签名挑战并取得 ClientID。
3. 管理员创建隧道，平台立即分配 TunnelID；设备离线时也能保存期望配置。
4. Control 确认 Edge 授权策略后下发完整设备配置。
5. 客户端建立 Provider，Visitor 使用独立凭据通过官方 gost 访问固定目标。
6. 修改、停用、删除和撤销通过新版本协调，失效授权对应的活动连接被关闭。

## 能力与边界

| 已实现能力 | 说明 |
| --- | --- |
| 设备身份 | 一次性注册、丢失响应恢复、稳定 ClientID、撤销和重复实例检测 |
| 隧道管理 | IPv4 TCP/UDP、创建/编辑/启停/删除、资源版本检查 |
| 访问凭据 | Provider/Visitor 分离、TunnelID 限制、Visitor 到期与再次展示 |
| 配置协调 | 持久 revision/hash、Edge 确认、operation/outbox、离线后收敛 |
| 运行管理 | 单实例、差异应用、独立持久退避、IPC 状态查询 |
| 系统集成 | Linux 脚本/systemd 示例，Windows LocalService MSI/修复/升级 |
| 观测 | 审计、心跳、Bind/session 事件、Edge 指标和健康接口 |

当前没有多租户、管理员细粒度 RBAC、多 Edge、高可用调度、P2P、TUN、远程 shell、浏览器直访业务、计费或自动升级。

目标为具体 IPv4 单播地址和 1–65535 端口，不接受目标域名或 IPv6。拒绝未指定地址、组播和全局广播；是否可达由客户端网络决定。外部 origin 可使用非默认端口，443 是默认值而不是唯一允许值。

## 状态解释

| 状态 | 已证明 | 尚未证明 |
| --- | --- | --- |
| accepted / pending | 变更已进入数据库 | Edge 和设备已应用 |
| operation applied | 所需 Edge 策略已确认 | 设备在线、目标健康 |
| received_revision | 设备接收对应配置 | 所有运行对象已应用 |
| applied_revision | 配置运行对象已应用 | 数据连接已完成 Bind |
| 管理 online | 管理 WSS 有新鲜心跳 | 所有数据隧道可用 |
| 隧道 connected | Provider 完成真实 Bind | 目标端口监听或业务响应正确 |
| 目标健康 unknown | 没有主动探测证据 | 不能据此断定目标故障 |

成功标准必须包含官方 gost 实际 TCP 内容读取、UDP 回显、错误授权拒绝、配置收敛、撤销关闭连接，以及身份与安装生命周期验证。不同验证层次见 [测试指南](testing.md)。
