# GOST 扩展维护

`third_party/gost-x` 基线为 `github.com/go-gost/x v0.16.0`，关联 `core v0.6.1`、官方命令行 `gost v3.3.0`。保留上游 MIT LICENSE，通过根目录 go.mod 的 replace 使用本地源码。没有修改原生 Visitor 的网络协议。

仅扩展以下边界：

- Tunnel Handler：解析认证、命令、TunnelID 后调用授权 Hook；追踪原连接，在 Bind 入池及 Connect 打开流前再次校验；Bind 成功回调。
- Service：可注入受上下文取消控制的精确重试策略；未设置时保留上游逻辑。
- Tunnel Connector：给 Bind 握手加整体 deadline，并在握手期间响应 context 取消；握手完成后解除 deadline 与取消回调。

`gost-x-v0.16.0.patch` 为相对上游源码的完整补丁，`manifest.json` 给出每个变更文件修改前后 SHA-256。新增 Hook 文件没有上游摘要。更新后执行：

```sh
node scripts/gost-patches.cjs
go test ./...
# 设置 PostgreSQL / 官方 GOST 测试环境后执行全部兼容测试
go test -race -count=1 ./...
```

重建补丁需要 Go 模块缓存、Git 与 Node。脚本从 Go checksum 校验的固定版本恢复基线，不能对第三方目录执行整体 gofmt，否则会产生与功能无关的大量差异。升级 GOST 时应先比较上游 Bind/Connect 生命周期、重跑原生 TCP/UDP、错误 TunnelID/角色、固定目标与连接吊销测试，再更新基线和摘要。
