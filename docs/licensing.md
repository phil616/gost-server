# 许可证与分发约束

核查日期：2026-10-19。依据当前 Go 依赖、npm 锁文件、生产 Webpack 模块清单和实际许可证正文。依赖升级必须重新核查，不能沿用本次结论覆盖未来版本。

## 本项目选择 MIT

项目原创代码、脚本、文档和对 GOST 的原创修改使用根目录 [MIT License](../LICENSE)，署名 `Copyright (c) 2026 @phil616`。第三方代码继续适用自己的许可；根许可证不改变其版权或要求。

| 选择 | 适用性 | 结论 |
| --- | --- | --- |
| MIT | 允许商业使用、修改、分发及再许可，要求保留版权和许可正文，包含免责条款 | 本仓库采用；与 GOST 的许可一致，便于复用 |
| Apache-2.0 | 同样宽松，另外明确规定贡献者专利授权、专利诉讼终止及修改/NOTICE 的保留要求 | 也是可行选择；若以后需要专利条款，可单独讨论后续原创版本的许可 |

MIT 的准确条件见 [MIT 正文](https://opensource.org/license/mit)；Apache 的条件见 [Apache 2.0 正文](https://www.apache.org/licenses/LICENSE-2.0)。本项目没有添加“禁止商用”“必须署名 UI”等额外限制，也没有额外 EULA 覆盖第三方权利。

## 上游是否约束整个仓库

上游约束依然存在，但当前依赖允许本项目原创部分采用 MIT，不要求将整个仓库改成 MPL 或 GPL。

| 组件 | 固定版本或来源 | 许可与处理 |
| --- | --- | --- |
| GOST core / x / relay 等 | 以 go.mod 和 third_party/gost-x 为准 | MIT；保留 ginuerzh 等上游版权和正文，修改与基线在 patches/ 中记录 |
| hashicorp/hcl | v1.0.0 | MPL-2.0；未修改，分发完整对应源码 ZIP 和原许可 |
| hashicorp/yamux | v0.1.1 | MPL-2.0；未修改，分发完整对应源码 ZIP 和原许可 |
| 其他链接的 Go 模块 | 当前 Windows amd64、Linux amd64/arm64 构建依赖 | MIT、BSD、ISC、Apache-2.0、Zlib；收集原许可、NOTICE、PATENTS 和嵌套许可 |
| 实际进入生产编译的 npm 模块 | Webpack 模块证据及 package-lock.json | 当前为 MIT / Apache-2.0；保留正文与版权 |
| npm 构建/测试工具 | 仅在构建机器安装 | 不将整个 node_modules 作为产品分发；构建工具许可不自动改变其输出的许可 |

GOST 的版本许可可在 [x v0.16.0](https://github.com/go-gost/x/blob/v0.16.0/LICENSE) 与 [core v0.6.1](https://github.com/go-gost/core/blob/v0.6.1/LICENSE) 核对。HashiCorp 后续其他产品/版本采用的许可不能套用到这些固定的旧版本上；这里依据 [HCL v1.0.0](https://github.com/hashicorp/hcl/blob/v1.0.0/LICENSE) 和 [yamux v0.1.1](https://github.com/hashicorp/yamux/blob/v0.1.1/LICENSE) 正文。

MPL 是文件范围的 copyleft：受其覆盖的源码及修改仍按 MPL 提供，独立原创文件可以继续 MIT；静态链接不自动要求其他文件采用 MPL。分发二进制时还需告诉接收者如何取得对应 MPL 源码。依据 [Mozilla 官方 FAQ，Q8–Q13](https://www.mozilla.org/en-US/MPL/2.0/FAQ/)，发布包直接携带经 Go 模块校验的原始模块 ZIP，避免仅提供可能失效的上游链接。未来如修改这些模块，必须改为分发实际修改后的对应 MPL 源码。

## AGPL 开发工具的边界

锁文件含 Umi 间接依赖 `@stagewise/toolbar@0.6.2`（AGPL-3.0-only）。当前将 `stagewise` 显式关闭，生产编译记录中没有它；发布包不包含其源码或 node_modules。不能据此宣称“所有安装依赖都是宽松许可”。

`web/license-manifest-plugin.cjs` 记录生产编译的模块包路径，包括合并模块；收集脚本对路径、锁定版本、实际许可正文进行核对。发布许可白名单不接受 AGPL/GPL 或未知许可。如果将来打开开发插件或更换打包器，必须重新审查模块证据及内嵌预编译代码；不能只按 package.json 的 dependencies/devDependencies 分类。

当前 171 个分发组件是 Windows 上本次生产编译的核查结果，不是所有安装依赖总数，也不是正式法律认证。除编译模块外，显式保留 Umi 内嵌 Webpack 5.88.2 生成的运行时代码的 MIT 许可；该模板代码没有 module.resource，不能只靠资源路径收集。编译图是保守范围，可能包含最后被 tree shaking 删除的模块；预打包进某个包的第三方代码还依靠其内部 LICENSE/NOTICE 和上游归属说明保留。

## 每种交付物应包含什么

- 仓库源码：根 LICENSE、THIRD_PARTY_NOTICES.md、GOST 原许可证、补丁、Go/npm 锁文件及当前文档。
- 完整分发 ZIP：上述使用材料、生产 Web、三个平台程序和 MSI、聚合第三方许可、结构化清单、MPL 源码 ZIP、本项目完整源码 ZIP、提交与版本元数据、逐文件校验值。
- 独立源码 ZIP：项目可修改源文件、完整 vendored GOST、锁文件与工作流；不含运行秘密、依赖安装目录或构建工具二进制。Go/npm 外部依赖由锁定版本获取。
- MSI：客户端程序、根 MIT LICENSE、第三方声明和 `licenses.zip`；该压缩包内有许可清单、正文及对应 MPL 源码。即使 MSI 被单独转发，接收者仍有这些材料。
- Docker 服务端镜像：`/usr/share/altasci/` 内含项目 LICENSE、第三方声明、聚合许可/清单及 MPL 模块源码，`sources/project-source.tar.gz` 提供项目构建源码；基础镜像自带的系统许可一并保留。Go/npm 清单不宣称覆盖 Distroless 系统材料，详见 [Docker 指南](docker.md)。

`licenses/dependency-inventory.json` schemaVersion=2 记录组件版本、许可、来源、npm integrity 及 MPL 模块 checksum / 源码 SHA-256；不是 SPDX/CycloneDX 格式 SBOM。`THIRD_PARTY_LICENSES.txt` 包含原始许可/NOTICE。保留上游版权，不把聚合清单理解为“所有第三方代码重新以 MIT 许可”。

## 自动检查与维护

```sh
node scripts/collect-licenses.cjs web/dist/licenses web
node scripts/verify-licenses.cjs web/dist/licenses
```

先完整安装依赖并执行生产编译。清单缺失、未知生产许可、缺少正文、锁文件版本不符或缺少 MPL 源码都会失败。某些包把完整 MIT 正文放在 README 中，收集器保留该文件；单独一个 SPDX 字符串不足以代替正文。

本次不是更改 GitHub 仓库可见性的操作；许可证文件在提交后使代码具备明确开源授权。公开发布还需将这些文件推送到 public 仓库。当前本地没有配置 Git remote，远端可见性与 Actions 执行状态需由仓库创建/推送后确认。操作见 [发布指南](releasing.md)。
