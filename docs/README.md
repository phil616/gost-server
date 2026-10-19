# 文档中心

本文档体系描述仓库当前实现。产品范围、架构、操作和验证分开维护；固定版本测试记录不作为后续代码自动通过验收的证明。

## 阅读路径

| 目标 | 推荐顺序 |
| --- | --- |
| 自有服务器临时测试 | [快速开始](getting-started.md) → [使用指南](usage.md) → [故障排查](troubleshooting.md) |
| 单容器运行服务端，复用既有 PostgreSQL 和反代 | [Docker 启动指南](docker.md) |
| 接入现有数据库、证书和反代 | [配置参考](configuration.md) → [安全设计](security.md) |
| 理解或修改系统 | [项目概览](overview.md) → [系统架构](architecture.md) → [技术实现](implementation.md) → [数据模型](data-model.md) |
| 调用 API 或开发管理客户端 | [接口说明](api.md) → [OpenAPI](openapi.yaml) → [管理协议](agent-protocol.md) |
| 构建、测试和维护 | [开发指南](development.md) → [测试指南](testing.md) → [运行维护](operations.md) |

## 完整目录

| 文档 | 维护内容 |
| --- | --- |
| [项目概览](overview.md) | 目标、角色、术语、功能边界与验收标准 |
| [系统架构](architecture.md) | 组件图、连接方向、关键时序、故障边界 |
| [技术实现](implementation.md) | 模块、配置协调、客户端运行时、前端和 GOST 集成 |
| [数据模型](data-model.md) | 实体、关系、版本、事务、索引和保留策略 |
| [安全设计](security.md) | 身份、秘密、授权、TLS、IPC 和已知限制 |
| [架构决策](decisions.md) | 已采用的取舍、原因及重新评估条件 |
| [快速开始](getting-started.md) | 使用既有基础设施的最小启动与首轮验证 |
| [Docker 启动指南](docker.md) | 单镜像 Control/Edge/Web、Compose、外置数据库、初始化、持久化和网络 |
| [使用指南](usage.md) | 注册、TCP/UDP 访问、编辑、撤销和安装 |
| [配置参考](configuration.md) | CLI、默认值、环境、文件、反代和健康接口 |
| [接口说明](api.md) | REST 调用、CSRF、幂等、并发控制与错误 |
| [OpenAPI](openapi.yaml) | REST 路径、参数和机器可读接口说明 |
| [管理协议](agent-protocol.md) | 设备 WSS 消息、认证、快照、ACK 和心跳 |
| [开发指南](development.md) | 目录、环境、构建、生成、补丁和发布包 |
| [许可证说明](licensing.md) | MIT 选择、上游约束、MPL 源码、生产依赖与分发材料 |
| [发布指南](releasing.md) | push CI、产物结构、版本标签、GitHub Release 与权限 |
| [测试指南](testing.md) | 用例层次、依赖、跳过项、CI、压力与长期验证 |
| [系统安装验收](system-acceptance.md) | 可丢弃环境中的 MSI/systemd 生命周期验收 |
| [运行维护](operations.md) | 运行管理、观测、升级、备份和旧库恢复 |
| [故障排查](troubleshooting.md) | 按现象定位连接、授权、配置和目标问题 |
| [版本验证记录](validation/README.md) | 固定版本证据索引与适用范围 |
| [仓库维护](maintenance.md) | 生成物清理、保留规则和文档检查 |

## 来源与维护规则

参数以 `cmd/*/main.go` 为准，API 以 `internal/control/http.go` 为准，数据结构以模型和 SQL 迁移为准。未来计划不能写成已经实现的能力。新增功能同时更新实现说明、接口和有效测试。

反代产品由使用者选择，`deploy/openresty.conf` 只是可选示例。根目录 `.env` 不会被程序自动读取，秘密不得写入文档。内部链接采用相对路径，执行 `node scripts/check-docs.cjs` 检查引用。

历史验证保留原日期、平台、跳过项和限制。新版本单独记录，不把旧结果覆盖成新版本的结论。
