# 管理 REST API

## 入口与约定

外部 base URL 为 `https://YOUR_ORIGIN/api/v1`。机器可读路径/参数见 [OpenAPI](openapi.yaml)，路由实现见 `internal/control/http.go`。JSON 返回默认带 `Cache-Control: no-store`，错误主体为 `{"error":"..."}`。

管理 API 使用 Cookie 会话。除登录外，接口需要有效会话；写操作需要 X-CSRF-Token，浏览器请求还校验存在的 Origin 是否匹配完整 Host。不要把设备的 WSS 注册 token 当作管理员 API bearer token。

## 认证调用

先登录保存 Cookie，并从 JSON 取出 csrf：

```sh
curl --fail-with-body -c cookies.txt \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"YOUR_TEST_PASSWORD"}' \
  https://YOUR_ORIGIN/api/v1/auth/login
```

示例中的密码仅用于说明；真实脚本应从受限配置或标准输入构造请求，避免将密码留在共享历史/日志。返回 `{ "username": "admin", "csrf": "..." }`。后续写请求使用 `-b cookies.txt -H 'X-CSRF-Token: ...'`。`GET /auth/me` 可以恢复当前会话及 CSRF；`POST /auth/logout` 删除会话。

## 路径索引

| 方法 / 路径（省略 /api/v1） | 用途 | 写入结果 |
| --- | --- | --- |
| POST `/auth/login` | 创建管理员会话 | 200 + Cookie/CSRF |
| GET `/auth/me` | 当前管理员与 CSRF | 200 |
| POST `/auth/logout` | 注销 | 200 |
| POST `/enrollments` | 创建注册申请，token 仅返回一次 | 201，含 token、expires_at、命令 |
| DELETE `/enrollments/{id}` | 撤销申请 | 200 |
| GET `/clients`、`/clients/{id}` | 设备列表/详情 | 不返回身份公钥等内部材料 |
| POST `/clients/{id}/revoke` | 撤销设备 | 202 operation |
| POST `/clients/{id}/tunnels` | 分配 TunnelID 和期望配置 | 202 operation |
| GET `/tunnels`、`/tunnels/{id}` | 隧道列表/详情 | 详情带数字版本 ETag |
| PATCH `/tunnels/{id}` | 修改名称、协议、目标、启用状态 | 202，要求 If-Match |
| DELETE `/tunnels/{id}` | 逻辑删除隧道 | 202 operation |
| GET/POST `/tunnels/{id}/access-credentials` | 列举/创建 Visitor | 创建返回 202 operation |
| POST `/access-credentials/{id}/reveal` | 审计并展示用户名、秘密、命令和 YAML | 200，敏感响应 |
| DELETE `/access-credentials/{id}` | 撤销 Visitor 及其活动连接 | 202 operation |
| GET `/operations/{id}` | 查询持久操作状态 | pending/applied |
| GET `/audit-events` | 新到旧审计列表 | 分页 |
| GET `/edge/status` | Edge 快照与健康错误 | revision、hash、epoch、sequence、sessions |

## 创建与编辑隧道

```json
{
  "name": "test-http",
  "protocol": "tcp",
  "target_ip": "127.0.0.1",
  "target_port": 18090,
  "enabled": true
}
```

POST 到 `/clients/CLIENT_ID/tunnels`。协议只接受 tcp/udp，目标是具体 IPv4 单播地址，端口为 1–65535。HTTP 202 响应形如：

```json
{
  "operation_id": "OPERATION_ID",
  "resource_id": "TUNNEL_ID",
  "policy_revision": 12,
  "state": "pending"
}
```

查询 operation 直到 Edge 确认策略；随后分别检查设备的 received/applied revision 和隧道连接。API 接受不代表目标业务已经可用。

PATCH 前读取资源当前 version，以 `If-Match: 3` 等数字资源版本提交。并发冲突返回 409，调用方应重新读取并让用户决定新的修改，不能无条件覆盖其他管理员的变化。

## 幂等

支持的资源变更可传 `Idempotency-Key`，按管理员隔离。相同 key + 同方法/路径/请求体字节复用原结果；不同内容复用同 key 返回 409，包括 JSON 空格或字段顺序变化。对“响应丢失后重试”的同一请求应重用 key 与原始 body，新的用户动作应生成新 key。

登录、注册申请创建和 reveal 不应被假定具备所有资源 mutation 相同的幂等行为；以 OpenAPI 中实际参数声明为准。已完成 operation 的保留约 180 天，不承诺永久幂等去重。

## Visitor 与敏感数据

发放凭据时可传 `expires_at` 的 RFC3339 时间或省略。列表不返回秘密或有效校验材料；当前序列化仍包含空字符串 `digest`、`ciphertext` 兼容字段。新凭据的资源 ID 用于随后 reveal。reveal 返回：

```json
{
  "username": "visitor-...",
  "secret": "...",
  "command": "gost -L ... -F ...",
  "yaml": "services:\n..."
}
```

不要将该响应放进通用访问日志、前端遥测或公共文件。凭据只能访问所属 TunnelID，不是平台全局访问口令。

## 分页与错误

列表使用 `page`（默认 1）、`page_size`（默认 20，上限 100），通常返回 `items` 和 `total`；隧道列表支持 `client_id` 筛选。调用方按 total 或空页结束，不假定某次列表包含全部资源。

| 状态码 | 处理 |
| --- | --- |
| 400 | 请求或字段不合法，修正后再提交 |
| 401 | 登录/会话无效，重新认证 |
| 403 | CSRF/Origin 校验失败，检查会话和代理 Host |
| 404 | 资源不存在或不能展示 |
| 409 | 并发版本、幂等或业务状态冲突 |
| 429 | 登录预算/并发限制，按 Retry-After 等待 |
| 503 | 仓储或依赖不可用；保留请求 key，依语义重试 |

Control `/health/live` 和 `/health/ready` 不在 `/api/v1` 下。未知 GET 可能返回前端 HTML，调用者必须检查 Content-Type 和实际 JSON，不能仅据 HTTP 200 判断 API 存在。

## Edge 内部接口

以下只在 `--admin` 指定的受限 listener 上提供，不是外部 `/ws` 或 Control REST 的子接口：

| 方法 / 路径 | 说明 |
| --- | --- |
| GET `/internal/v1/policy` | 当前完整授权策略，含凭据校验摘要 |
| PUT `/internal/v1/policy` | 应用完整 policy，最大主体 16 MiB，冲突/非法策略返回 409 |
| GET `/internal/v1/runtime` | epoch、sequence、revision、policy_hash、sessions、updated_at |
| GET `/internal/v1/events` | NDJSON 快照流，按 sequence 变化输出；不是持久事件总线 |
| GET `/metrics` | Prometheus 文本指标 |
| GET `/health/live`、`/health/ready` | 进程与策略就绪检查 |

此入口依赖 OS 权限或本机信任，没有与管理员 REST 相同的 Cookie 认证。只让 Control 和受信任诊断工具访问，不把策略 PUT 路由代理到外部。

## 客户端本地 IPC

CLI 使用本地 HTTP 语义访问私有 socket/命名管道：`GET /status` 查询，`POST /register` 提交 `token` 和可选 `server`。状态含 client_id、state、error、管理 retry 和 runtime；私钥不通过 status 返回。

Windows 固定管道 `\\.\pipe\AltasCITunnel`，Linux 为数据目录下 `agent.sock`。它们不是普通 TCP 服务或供浏览器远程调用的 API。优先使用 register/status CLI，维持操作系统权限与单实例语义。
