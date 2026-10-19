# 运行维护

## 前台测试与服务管理

最小联调按 [快速开始](getting-started.md) 在两个终端运行，Ctrl+C 停止。设备与 Visitor、数据库和反代由各自环境管理；无须为临时测试注册开机服务。

需要长期测试或平台安装验收时，可以采用 `deploy/systemd` 示例。它们假定服务器程序和 Web 位于 `/opt/altasci`，主密钥与 Control 环境文件位于 `/etc/altasci`。这些是模板默认值，不是应用固定部署要求。

| 模板 | 用户 / 组 | 写目录与权限 |
| --- | --- | --- |
| altasci-edge.service | altasci-edge / altasci | `/run/altasci` 0750，`/var/lib/altasci-edge` 0700 |
| altasci-control.service | altasci-control / altasci | 通过共享组访问 Edge socket，读取密钥/环境 |
| altascitunnel.service | altascitunnel / altascitunnel | `/var/lib/altascitunnel` 0700 |

Control 与 Edge 都需要所属 `altasci` 组；socket 为 0660，父目录组权限同样需要允许访问。主密钥可由 root:altasci 持有并设为 0640，实际权限按运行账号分配。不要在已有密钥上重新执行 keygen。

Control unit 的 EnvironmentFile 为 `/etc/altasci/control.env`，其中 DATABASE_URL 直接传给程序，ALTASCI_PUBLIC_URL 由 unit 转换为 CLI 参数。`WorkingDirectory=/opt/altasci` 使默认 `web/dist` 正确解析。自定义路径必须同时调整参数、权限和 unit 的文件系统限制。

systemd 客户端安装脚本使用固定默认路径和用户；需要私有 CA 时通过本次 unit override 给 run 添加 `--ca-file`，并确保该账号可读取。Windows 可通过受信任的机器证书库或显式服务参数提供 CA；不要只给 register 加 CA 参数并期待已运行服务自动修改。

## 观测

前台程序输出结构化日志。服务端 systemd 示例使用 `LogNamespace=altasci`：

```sh
journalctl --namespace=altasci -u altasci-control -u altasci-edge -n 100
journalctl -u altascitunnel -n 100
curl --fail --unix-socket /run/altasci/edge.sock http://local/metrics
```

使用日志命名空间时可把 `deploy/journald@altasci.conf` 安装为 `/etc/systemd/journald@altasci.conf`，设置约 14 天/1 GiB 日志限制。保留规则属于运行环境，不是 Go 程序自动配置的能力。

Edge 指标包含 Provider/Visitor 连接数、拒绝认证数、Relay 收发字节和策略版本。字节计数随进程生命周期变化，不是计费账本。Control ready 只检查数据库可读，Edge ready 检查有有效版本策略；端点列表见 [配置参考](configuration.md#健康与指标)。

## 升级与回退

1. 确认新版本实际测试范围与协议/数据库变更，备份数据库和配套主密钥。
2. 保留客户端身份与配置目录。Linux 安装脚本覆盖程序后显式重启；Windows 使用更高版本 MSI，修复和升级保留身份。
3. Control/Edge 升级按自己的进程管理体系执行。停止 Edge 会中断数据连接，停止 Control 则暂停管理但不必关闭现有数据流。
4. 显式运行目标版本 migrate，再启动组件；检查策略 revision/hash、operation、设备配置与真实业务流量。
5. 数据库迁移后的二进制回退只在确认 schema/协议兼容时进行；恢复旧库还必须执行下述版本恢复。

没有自动升级、自动回滚或蓝绿切换功能。前端包版本、MSI 产品版本、设备管理协议 `v=1` 是不同概念，不能根据单一显示值判断所有组件兼容。

## 备份

以现有数据库体系备份独立业务库，例如 `pg_dump --format=custom`；密码通过已有连接配置/pgpass 提供。单独备份 Vault 主密钥，按秘密管理材料保管，不把数据库与唯一密钥副本混成一个不可恢复依赖。

Edge 策略可以从健康 Control 重建，但仍包含授权摘要，不能公开。设备 identity.json 含私钥，config.json 含 Provider 凭据；需要保留设备身份时限制备份读取者，不把同一设备备份复制成多个并行实例。

定期在隔离环境恢复数据库与配套密钥，验证解密和资源关联。备份成功的退出码不能证明恢复结果可运行。

## 恢复旧数据库

1. 停止 Control 和 Edge，暂时关闭本次数据入口。恢复数据库及与之匹配的主密钥。
2. 确定高于所有曾发出的全局策略版本和设备配置版本的整数。通常 Unix 毫秒时间足够，但曾手工设置更大版本时必须继续递增；上限为 JSON/UI 安全整数 9007199254740991。
3. 设置恢复库 DATABASE_URL 后运行：

```sh
altasci-control recover-revisions \
  --key /path/to/restored/secrets.key \
  --minimum-revision YOUR_HIGHER_REVISION
```

该事务撤销所有 Visitor、轮换 Provider、递增 generation、提高各设备配置与全局策略版本，保留已有设备身份。命令不会读取 pg_dump 文件，也不自动推断所有历史版本上界。

4. 在入口仍关闭时启动 Edge/Control，确认新策略 revision/hash 及 operation applied，再开放接入。
5. 重新发放 Visitor。备份之后才注册、恢复库中不存在的设备需要撤销旧记录/清理其测试身份后重新注册；恢复库内仍存在的设备保留私钥，不需要重注册。

直接开放旧数据库与旧 Edge 策略可能恢复本已撤销的访问权限。仅提高一个全局字段而不轮换授权不能完成此恢复流程。

## 卸载与测试清理

Linux `scripts/uninstall-linux.sh` 停止服务并移除程序/unit，默认保留客户端身份；`--purge` 清理固定客户端目录。脚本拒绝对符号链接状态目录执行 purge。Windows MSI 正常卸载清理身份/配置，升级卸载路径保留状态。

停止前台测试不需要调用服务卸载命令。只删除本次明确创建的测试目录、数据库或临时反代规则；仓库缓存、构建物清理使用 [维护规则](maintenance.md)，不能据文件名相似删除真实设备身份或数据库。
