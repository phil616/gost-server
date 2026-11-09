# 查询参数解码器兼容补丁

`query-string 6.14.1` 是 `@umijs/history 5.3.1` 的依赖，使用 CommonJS 调用解码器。`package.json` 将 `decode-uri-component` 固定到修复两个拒绝服务漏洞的 `0.5.1`，该版本只导出 ESM default；补丁使旧调用方读取 `.default`，保留现有查询参数与路由接口。

`npm ci` / `npm install` 的 postinstall 自动通过 `patch-package --error-on-fail` 应用补丁，失败即终止安装。开发和构建使用 Node 24（支持同步 require ESM）。Docker 安装前也复制此目录；不要使用 `--ignore-scripts` 跳过补丁。

升级 Umi 或 query-string 时，先检查调用方式，再更新或移除补丁。运行 `npm test` 验证 Umi 查询参数解析、导航、CommonJS 互操作和两类畸形输入，运行 `npm run build` 验证浏览器构建。

漏洞公告：[GHSA-vcc3-ghjq-m6fr](https://github.com/advisories/GHSA-vcc3-ghjq-m6fr)、[GHSA-qr28-fpjm-wq7f](https://github.com/advisories/GHSA-qr28-fpjm-wq7f)。本补丁只修改模块接口适配，解码算法直接使用上游发布包。
