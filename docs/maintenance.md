# 仓库维护

保留源码、锁文件、迁移、有效测试、第三方许可证与补丁。生成物和日志不入库；配置、设备身份、密钥和数据库按运行数据管理。

## 清理生成物

使用 PowerShell 7，在没有构建或服务写入目标目录时执行。先加 `-WhatIf` 查看范围：

```powershell
./scripts/clean.ps1 -WhatIf
./scripts/clean.ps1 -Dependencies -BuildOutputs -WhatIf
```

| 开关 | 清理范围 |
| --- | --- |
| 默认 | Umi 缓存、Python 缓存、security-audit 临时报告 |
| `-Dependencies` | web/node_modules |
| `-FrontendOutput` | web/dist |
| `-BuildOutputs` | bin、dist、web/dist，包含发布归档 |
| `-LocalTools` | .local，包括本地工具和草稿，按需选择 |
| `-Staging` | 发布解压目录、WiX 调试文件和许可中间包 |
| `-OldRelease X.Y.Z` | 指定版本的发布归档、MSI、摘要和 staging |

## 文档检查

```sh
node scripts/check-docs.cjs
git diff --check
```

新增 API、参数或协议时同步对应文档。测试输出保存在 CI artifacts 或本地忽略目录，不提交原始日志和一次性审查报告。
