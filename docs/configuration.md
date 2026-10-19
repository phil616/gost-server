# 配置参考

## 配置来源

CLI 参数以 `cmd/*/main.go` 为准。`DATABASE_URL` 是 Control 直接读取的环境变量；`ALTASCI_PUBLIC_URL` 由示例 systemd unit 展开后传给 `--public-url`，不是程序自动读取的同名变量。

程序没有 dotenv 自动加载。根目录 `.env` 只是用户自己的配置文件，必须由 shell 或进程管理器明确导出需要的值。显式 `--database-url` 会覆盖环境值；不建议把数据库密码放进进程参数。

Docker 的 `altasci-server` 入口额外读取 `ALTASCI_PUBLIC_URL`、`ALTASCI_DATA_DIR`、`ALTASCI_CONTROL_LISTEN`、`ALTASCI_EDGE_LISTEN` 并转换为两个服务进程的参数；Compose 用显式 `--env-file compose.env` 注入，见 [Docker 启动指南](docker.md)。

## altasci-control

命令为 `serve`（默认）、`keygen`、`migrate`、`admin-create`、`recover-revisions`。参数应放在命令之后。

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| `--database-url` | 环境 `DATABASE_URL` | 除 keygen 外需要 PostgreSQL 连接串 |
| `--key` | `/etc/altasci/secrets.key` | 32 字节 AES 主密钥 |
| `--listen` | `127.0.0.1:18081` | HTTP/管理 WS，默认仅字面量 loopback |
| `--allow-non-loopback` | false | 显式允许业务入口监听容器/私网字面量 IP；TLS 仍由反代处理 |
| `--edge` | `unix:///run/altasci/edge.sock` | Edge 内部接口；TCP 形式为 `http://127.0.0.1:18082` |
| `--public-url` | `https://tunnel.example.com` | 外部 HTTPS origin，用于注册及 Visitor 导出 |
| `--web` | `web/dist` | 前端静态资源目录，建议绝对路径 |
| `--username` | `admin` | admin-create 的用户名 |
| `--password-stdin` | false | admin-create 必须显式启用，从 stdin 读一行 |
| `--dev-http` | false | 开发时允许非 Secure 管理 Cookie |
| `--minimum-revision` | 0 | 恢复命令所需版本下界，必须为有效正整数 |

keygen 只生成新密钥，拒绝覆盖。migrate 运行嵌入程序的 goose SQL。恢复命令不负责从备份导入数据库，见 [恢复流程](operations.md#恢复旧数据库)。

## altasci-edge

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| `--listen` | `127.0.0.1:18080` | GOST WS 数据入口，仅字面量 loopback |
| `--allow-non-loopback` | false | 显式允许数据入口监听容器/私网字面量 IP；不影响 `--admin` |
| `--admin` | `unix:///run/altasci/edge.sock` | 内部管理 listener，也可为 `127.0.0.1:18082` |
| `--policy` | `/var/lib/altasci-edge/policy.json` | 持久策略快照 |

Edge 侧 TCP `--admin` 不带 `http://`；Control 侧 HTTP 客户端的 `--edge` 则需要 URL scheme。Unix socket 形式两侧一致，建议绝对路径。

## altascitunnel

| 命令 | 行为 |
| --- | --- |
| `run` | 前台运行或作为系统服务运行，默认命令 |
| `register` | 向已运行实例的 IPC 提交注册 token |
| `status` | 读取已运行实例的 IPC 状态 |
| `service install/start/stop/uninstall` | 系统服务操作；完整卸载清除身份 |
| `service-prepare`、`purge-state` | MSI 使用的内部命令，不作为日常操作入口 |

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--data-dir` | Linux `/var/lib/altascitunnel`；Windows `%ProgramData%\AltasCITunnel` | 私有状态目录 |
| `--server` | 初次默认 `wss://tunnel.example.com`，已有身份优先用持久 origin | 只接受 origin，不带 `/agent/v1/ws` |
| `--ca-file` | 空 | run/service install 的额外信任 CA 文件 |
| `--token` | 空 | register 必需的一次性 token |
| `--allow-loopback-ws` | false | 显式允许 loopback WS，仅本机测试 |

CA 参数需要给运行实例，而不是只给 register 命令；后者只是 IPC 客户端。注册后的 origin 受身份约束，更改服务器不能绕过原设备身份。Windows 命名管道与 mutex 为全局单实例，不因 data-dir 不同允许多个并行进程。

## 持久文件

| 位置 / 文件 | 内容 | 生命周期 |
| --- | --- | --- |
| Control 主密钥 | 原始 32 字节密钥 | 与数据库配套备份，重启升级保留 |
| Edge policy.json | 授权摘要、revision、hash | Edge 重启恢复；不含 Visitor 明文 |
| 客户端 identity.json | 私钥、ClientID、request_id、origin，注册中可含 token | 身份核心，不能随意复制或删除 |
| 客户端 config.json | 最近配置及 Provider 秘密 | 私有目录内保存 |
| management-retry.json | 管理链路 count/next | 成功清零，进程重启延续 |
| retries.json | 按隧道的 count/next | 独立持久退避 |
| Linux agent.sock / agent.lock | IPC 和 flock 文件 | 运行与单实例控制 |

本地测试可指定独立目录。Windows 仍必须遵循全局单实例；Linux 服务安装脚本使用固定默认目录，不能据通用 CLI 参数推断脚本也支持任意目录。

## 反代与网络契约

1. 外部提供 HTTPS/WSS origin，路径 `/ws`、`/agent/v1/ws` 精确路由到对应后端，其他流量进入 Control。
2. 保留外部完整 Host，包括非默认端口；管理写操作校验 Origin host:port。不要通过随意删除 Origin 头绕过这个检查。
3. 两条 WS 路径支持 Upgrade、双向流式转发和长连接；避免缓存/缓冲。心跳为 15 秒，超时需留出网络余量。
4. 证书名称与 origin 一致；设备/Visitor 信任签发 CA。证书在反代侧管理，应用内部 HTTP listener 不接受 TLS 证书参数。
 5. 业务后端默认只能绑定字面量 loopback。容器内通过显式 `--allow-non-loopback` 允许 `0.0.0.0:18081` 等字面量 IP，借助 Docker 私网或回环端口发布接入；`localhost:18081` 仍不接受。Edge 内部管理监听不放宽。
6. `/internal/v1/*` 属于 Edge 管理入口，不能加入外部数据入口路由。

`deploy/openresty.conf` 仅为可选示例。非默认外部端口使用该示例时，应让 Host 转发保留端口，例如改成完整外部请求 Host，而不是丢弃端口的主机名。

## 健康与指标

| 组件 | 路径 | 语义 |
| --- | --- | --- |
| Control | `/health/live` | HTTP 进程存活 |
| Control | `/health/ready` | 能读取仓储；不证明 Edge 或所有设备健康 |
| Edge 内部入口 | `/health/live` | 内部管理服务存活 |
| Edge 内部入口 | `/health/ready` | 已有 revision 至少为 1 的策略 |
| Edge 内部入口 | `/metrics` | Provider/Visitor 数、拒绝认证、字节与策略版本 |

没有 `/api/v1/health`。健康检查与指标无需管理员 Cookie，但 Edge 接口受本机/Unix socket 隔离。Web 的 `/api/v1/edge/status` 需要管理会话。
