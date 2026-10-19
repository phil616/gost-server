# 参与贡献

维护者：[\@phil616](https://github.com/phil616)。欢迎通过 Issue 描述问题，通过 Pull Request 提交改动。

提交前阅读 [开发指南](docs/development.md)、[测试指南](docs/testing.md) 和 [许可证说明](docs/licensing.md)。报告应包含版本、系统、复现步骤、预期与实际结果；删除设备私钥、访问凭据、数据库密码和注册 token。安全漏洞按照 [安全报告规则](SECURITY.md) 处理。

## 提交要求

- 说明具体问题、行为变化、验证命令、结果与未覆盖范围；测试跳过不能算作通过。
- 修改 CLI、协议、授权或数据结构时同步文档和必要测试。
- 使用现有锁文件；引入依赖前检查许可证。GOST 改动同时维护 [补丁记录](patches/README.md)。
- 不提交运行身份、真实配置、node_modules、数据库、临时工具和发布归档。

最低检查：

```sh
node scripts/check-docs.cjs
node --test scripts/release-tools.test.cjs
go test -count=1 ./...
go vet ./...
# 在 web/ 内：npm ci、npm run typecheck、npm test、npm run build
```

Pull Request CI 还会执行 PostgreSQL、官方 GOST、浏览器与 race 验证，以及 Windows 测试和打包。安装与长期运行另按 [验收手册](docs/system-acceptance.md) 执行。

## 贡献许可

除非另有明确约定，提交本项目原创内容表示你有权提交这些内容，并同意按本项目 [MIT License](LICENSE) 发布。第三方文件保留原许可证和署名；不要将第三方代码的版权改成项目维护者。无需额外 CLA。
