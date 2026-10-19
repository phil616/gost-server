# 故障排查

从入口、管理连接、配置、Provider、Visitor 和目标服务分别定位；不要只依据“在线”或 HTTP 200 判断系统健康。

## 推荐检查顺序

1. Control `/health/live` 和 `/health/ready` 是否成功，启动日志是否有迁移/密钥/数据库错误。
2. Edge 内部 `/health/live` 是否可达，是否收到有效策略；检查 Web 的 Edge 错误和 policy revision/hash。
3. 客户端 status 的 ClientID、state、error、retry 和 runtime。
4. 客户端本机直接访问目标服务，确认协议/IP/端口和权限。
5. Visitor 是否使用当前有效凭据、正确 TunnelID、origin 和 CA，并观察实际业务返回。

## 常见现象

| 现象 | 原因与处理 |
| --- | --- |
| `DATABASE_URL or --database-url required` | 程序没有加载 `.env`；在运行 Control 的终端显式 export，或由进程管理器注入 |
| 数据库连通但提示表不存在 | 尚未对该库运行 migrate，或连接到了错误数据库/schema |
| vault/key 长度或解密失败 | 主密钥路径、权限或数据库对应关系错误；不能生成新密钥替代丢失旧密钥 |
| `internal listener must bind a literal loopback address` | 使用了 localhost、0.0.0.0 或公网地址；改为 127.0.0.1，跨命名空间另行转发 |
| Control 无法连接 Edge | Unix socket 路径不一致、服务未启动，或父目录/组权限不足；TCP 参数两侧 scheme 要正确 |
| Edge ready 返回 503 `no policy` | 空白系统还没有确认策略，或快照无效且 Control 未能同步；不要伪造 ready |
| 登录后写操作 403 `invalid origin` | 代理丢弃外部端口、改写 Host，或前端/后端 origin 不一致；保留完整 Host |
| 403 `invalid CSRF token` | 会话和 csrf 不配套、过期或前端没有恢复会话；重新登录或读取 auth/me |
| 登录 429 | 命中进程级预算/并发限制，按 Retry-After 等待并检查反复登录逻辑 |
| 404/HTML 而非 API JSON | 路径错误，落入静态 SPA 回退；使用已声明 API/健康路径并看 Content-Type |
| 客户端一直 unregistered | register 只是本地提交；确认运行实例、数据目录、token、实际 WSS 可达性 |
| `local service unavailable` | 没有运行实例、IPC 权限错误或 Linux data-dir 不一致；先启动 run/服务 |
| `agent already running` 或 mutex/pipe 错误 | Windows 全局单实例仍被占用，或 Linux 同目录已运行；不要靠更换 Win data-dir 绕过 |
| `device already registered` | 当前目录已有 ClientID；无需重复注册，确认正在使用原 origin 和身份 |
| 设备签名/注册失败 | token 过期/撤销/已消费，或恢复请求不匹配原私钥/request_id；按注册流程处理 |
| `duplicate_instance` | 服务端仍有该身份的健康会话，或身份目录被复制；先终止重复实例并等待会话释放 |
| TLS unknown authority | 将正确 CA 传给运行实例和 Visitor，或使用其信任库；只给 register 参数无效 |
| TLS 名称错误 | 使用的 IP/域名不在证书 SAN；修正证书或 origin，不关闭校验 |
| 管理 online、隧道不 connected | 继续看 runtime error、Provider 授权、退避与 Edge 状态，不能归因为前端显示 |
| connected 但业务失败 | 客户端到目标不通、服务没监听、协议不符；Bind 成功不证明目标健康 |
| operation pending | 数据库已接受，但 Edge 策略尚未得到匹配 revision/hash 的确认 |
| operation applied 但设备未更新 | 这是 Edge 确认；检查设备 received/applied、管理 WSS 和 runtime |
| 旧凭据失效 | 被撤销、到期、TunnelID 错误或设备/隧道停用；重新发放不等于复活旧凭据 |
| 服务恢复后客户端等待很久 | 持久退避上限为 3125 秒；进程重启不自动清零，查看 next_retry_at |
| Windows 安装后命令找不到 | 当前终端 PATH 未刷新；使用完整安装路径或新开终端 |
| Linux 升级后身份丢失 | 使用了新目录或主动 purge；覆盖安装本身应保留身份，查实际文件与备份 |

## 网络与代理

从客户端到外部 origin 必须能建立验证证书的 WSS。`/agent/v1/ws` 和 `/ws` 是两条不同路由，前者成功不证明数据入口正确。代理需要保持路径、Upgrade、双向长连接和完整 Host，避免 WS 缓冲与过短空闲超时。

非默认端口必须出现在 Control public-url 和客户端 server 中；原生 GOST 导出会生成完整 host:port。v0.1.1 已修复 Provider 缺省 443 的拨号问题，旧二进制仍可能复现。

## 收集证据

记录时间、版本、复现步骤、脱敏 origin、operation ID、TunnelID、配置 revision、policy revision/hash、客户端 state/runtime，以及关联日志。不要提交 token、secret、identity.json、主密钥或未脱敏的 GOST 完整连接 URL。

临时前台测试可停止客户端后，仅移走本次目录的重试状态以加快排查，再用相同身份和参数启动。不能据此把设备 identity/config 删除当作常规恢复方案；系统服务与真实设备先按维护流程处理。
