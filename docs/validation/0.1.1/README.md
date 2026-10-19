# v0.1.1 验证记录

原测试日期：2026-10-18。本记录在文档整理时迁移，未因此重跑或扩大验证范围。

## 环境

初始验证：Windows amd64、Go 1.27.0、Node.js 24.19.0、npm 11.17.0、PostgreSQL 18.6、官方 gost v3.3.0 和 Chrome。Linux：WSL2 Debian 13.7 amd64、Go 1.27.2、GCC 14.2、PostgreSQL 17.11、Nginx 1.26.3。Windows 安装使用 Sandbox，未在 Windows 宿主安装应用服务。

## 实际完成

| 范围 | 结果与限制 |
| --- | --- |
| Go 全量 | 配置真实 PostgreSQL、官方 GOST、浏览器开关后全部通过；含浏览器 integration 约 31 秒 |
| PostgreSQL | 迁移重复执行、回滚、并发、约束、控制 API 与加密配置；随机 schema 隔离 |
| 身份和授权 | token 有效期/一次消费/恢复、签名、幂等/CSRF、role/TunnelID/协议/generation、策略 hash、恢复工具 |
| TCP/UDP/WSS | 官方 GOST 实际转发、固定目标、自签 CA 正常验证、控制故障与活动连接撤销 |
| 精确重试 | 假时钟验证完整数列、持久状态、清零、取消和时钟回拨；不是数小时真实等待 |
| 前端 | 干净锁定安装、类型检查、2 个 API 测试、生产构建 |
| Chrome | 登录、申请、隧道增删改/启停/真实 Bind、Visitor 展示/YAML/撤销、审计/Edge/退出，无捕获运行错误 |
| Windows Sandbox | [23 项检查](windows-sandbox.json) 通过：首装、LocalService、IPC 注册、TCP/UDP WSS、崩溃恢复、修复、升级保留身份与卸载 |
| WSL Linux | [22 项检查](linux-system.json) 通过：真实 PostgreSQL + 三个 systemd 进程 + Nginx TLS443、注册/转发/故障恢复/升级/撤销/卸载重装 |
| Linux race | [最终输出](linux-go-race.txt)：13 个顶层用例通过，规模用例按开关跳过；go vet 通过，空 vet 输出不再单独保留 |
| 产物 | Windows、Linux amd64/arm64 构建；arm64 未实际运行；GOST 六文件补丁与许可证、依赖许可清单 |

Windows 操作输出见 [transcript](windows-transcript.txt)，程序/MSI 摘要见 [artifacts.json](artifacts.json)。浏览器截图见 [console.png](../../images/console.png)。不完整安装和大体积本机 MSI 日志已清理，不再把 `.local` 路径当作持久证据。

Windows 夹具使用内存控制库与客体内 TLS 服务；客户端和官方 GOST 为真实独立进程。Linux 使用真实 Control/Edge 可执行程序和 PostgreSQL。两者均验证证书。Nginx 测试不代表实际运行 OpenResty。

## 发现与修复

1. 默认 `wss://origin` 注册可成功，但 GOST Provider 地址缺省端口导致数据失败。补齐 wss=443、ws=80 后，真实 443 TCP/UDP 验证通过。
2. Linux 安装脚本 `systemctl enable --now` 未重启已有进程；改为 enable 后 restart，验证 PID 改变、身份保留、转发恢复。

验收脚本同时修正 PATH 末尾反斜杠比较和服务启动期间 502 等待。Sandbox MSI 签名检查延迟按 [微软仓库记录](https://github.com/microsoft/Windows-Sandbox/issues/68) 仅在客体关闭 Smart App Control 并刷新 CI，未更改宿主签名策略。

## 模拟规模

同一 Go 测试进程内创建 100 Runtime 和一个 Edge，建立 1000 反向连接；一个官方 gost 进程产生 100 并发 TCP 业务会话并验证回显。启动/连通约 30.594 秒，Go heap 约 223 MiB，goroutine 约 12710，测试总计约 32.72 秒。

该测试为本机 WS，不经过真实代理/TLS，不是 100 台独立主机；Go heap 不包含其他进程或操作系统 RSS，不能当作生产容量结论。

## 尚未完成

- 公网证书、真实 OpenResty 二进制及受防火墙限制的跨主机部署。
- 24 小时浸泡、长期内存/句柄趋势和真实设备网络故障。
- Windows 组织代码签名及签名后验收。

复现和新版本验证见 [测试指南](../../testing.md) 与 [系统验收](../../system-acceptance.md)。
