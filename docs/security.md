# 安全设计

## 信任边界

| 边界 | 身份与限制 |
| --- | --- |
| 管理员 → Control | 密码、会话 Cookie、CSRF 与 Origin |
| 设备 → Control | 一次性注册 token + Ed25519 挑战证明；后续仅设备签名 |
| Provider → Edge | 高熵凭据、设备、TunnelID、协议、generation、Bind 角色 |
| Visitor → Edge | 高熵凭据、TunnelID、Connect 角色、撤销与有效期 |
| CLI → 客户端 | 操作系统 IPC 权限与单实例锁 |
| Control → Edge | 本机 Unix socket 用户/组权限，或受信任 loopback TCP |

当前管理员拥有控制全部设备和目标的权限，没有租户隔离或细粒度管理员角色。获得管理权限意味着可以让设备访问其网络内的任意允许目标，不能把它当作不受信任用户的自助代理平台。

## 管理认证

管理员密码使用 Argon2id，当前参数为时间成本 3、内存 64 MiB、并行度 2，随机 16 字节 salt，32 字节输出。初始化密码至少 12 个字符，CLI 从标准输入读取。数据库中的会话 ID 是随机 token 的摘要；默认会话有效 12 小时。

Cookie 默认 Secure、HttpOnly；写操作需要 X-CSRF-Token，有 Origin 时还比较完整 host:port。因此高端口反代必须保留外部 Host。登录有进程级速率预算及并发限制，不信任任意转发 IP 头作为认证依据。

`--dev-http` 只用于本机开发场景，会关闭 Secure Cookie；远程测试仍使用已有 HTTPS 入口。

## 设备注册

注册 token 为随机 32 字节的 base64url 编码，保存 SHA-256 摘要，10 分钟有效且一次消费。设备先保存 Ed25519 私钥和 request_id，再签名当前连接的随机 nonce。服务端在 30 秒认证窗口内校验签名，原子创建或恢复设备身份。

已消费 token 的恢复要求同 request_id、公钥及有效新挑战签名。复制 token 不能恢复已注册身份，但复制客户端身份目录会复制私钥；本机锁与服务端会话检查不等于防管理员克隆或硬件绑定。

## 秘密存储

| 材料 | 持久形式 | 位置 / 权限 |
| --- | --- | --- |
| 管理员密码 | Argon2id | PostgreSQL |
| 注册 token / Cookie token | SHA-256 摘要 | PostgreSQL |
| Provider/Visitor 秘密 | 校验摘要 + AES-256-GCM 密文 | PostgreSQL |
| 包含 Provider 的配置快照 | AES-256-GCM 密文 | PostgreSQL |
| Vault 主密钥 | 原始 32 字节 | 独立文件；生成时 0600 |
| 设备私钥/已应用配置 | 本地身份与配置文件 | 客户端受限目录 |
| Edge 策略 | 摘要与授权元信息，不含原始秘密 | Edge 私有快照文件 |

AES-GCM 每次使用随机 nonce，以凭据/配置记录 ID 为 AAD；把密文移到另一个 ID 下不能通过认证解密。主密钥不由数据库派生，也不会自动轮换。主密钥丢失后无法再展示 Visitor 或恢复 Provider 配置秘密。

管理员再次展示 Visitor 会写审计，并返回 `Cache-Control: no-store`。导出的命令和 YAML 包含明文秘密，应按访问凭据保管。高熵凭据用 SHA-256 校验，不应把此做法复用于低熵用户密码。

## 授权与固定目标

Edge 分离 Provider Bind 与 Visitor Connect，拒绝错误角色、TunnelID、协议、generation、撤销或到期凭据。策略仅包含当前有效资源，活动连接在授权失效后主动关闭；关键握手阶段二次校验防止撤销竞态。

客户端实际拨号固定到平台配置。Visitor 即使提交另一个地址，也不能绕过这个目标约束。Edge 授权不能替代客户端目标网络本身的访问控制。

## 操作系统隔离

Windows 安装使用 LocalService、专属服务 SID、受限命名管道、数据目录 ACL 和全局 mutex。Linux 使用独立用户、0700 数据目录、0600 IPC socket 和 flock；Edge 管理 Unix socket 为 0660，父目录/共享组决定 Control 的访问权限。

进程管理员和本机 root 不属于隔离对象。loopback TCP 管理接口没有另一套应用认证，拥有同机访问能力的进程应视为受信任；优先用受限 Unix socket 分配访问范围。

## TLS 与运行限制

管理和数据 WSS 验证证书与 origin 主机名；测试私有 CA 使用 `--ca-file` 或 GOST CA 参数，不使用关闭证书校验作为正常流程。TLS 在现有反代终止，代理到 loopback 的链路由主机隔离保护。

撤销依赖策略传达到 Edge；Control/数据库停机时无法承诺新撤销实时生效。Edge 可独立处理已经知道的到期时间。单 Edge 没有可用性冗余；UDP 经 TCP/WebSocket 不具备原生 UDP 的丢包和拥塞特征。

当前 Windows 发布产物未完成组织代码签名。Sandbox 测试中临时关闭客体 Smart App Control 是验收环境处理，不是宿主机或真实部署的签名策略建议。
