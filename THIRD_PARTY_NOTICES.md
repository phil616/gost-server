# Third-party components

- Original AltasCI Tunnel code and documentation: MIT, copyright (c) 2026 @phil616; see [LICENSE](LICENSE). This does not relicense third-party components.
- GOST x v0.16.0 (MIT): vendored under `third_party/gost-x`; copyright (c) 2016 ginuerzh and upstream license retained. Local changes are recorded in [patches](patches/README.md).
- Linked Go modules include MIT, BSD, ISC, Apache-2.0, Zlib, and MPL-2.0 components. Upstream copyright, license, NOTICE and patent texts are collected for distributions, including nested notices.
- HashiCorp HCL v1.0.0 and yamux v0.1.1: MPL-2.0, unmodified. Binary distributions include their complete, checksum-verified module sources in `licenses/sources/`, with hashes in `licenses/dependency-inventory.json`. These sources remain available under MPL-2.0; the project's MIT license does not restrict those rights.
- Production Webpack modules are matched to `web/package-lock.json`. Their license texts are included in `licenses/THIRD_PARTY_LICENSES.txt`; this includes complete grants embedded in upstream README files where applicable.
- Webpack 5.88.2, vendored in the pinned Umi bundler, contributes generated runtime code. Its MIT license (JS Foundation and other contributors) is explicitly included even though that code has no module.resource entry.
- Umi's indirect `@stagewise/toolbar` dependency is AGPL-3.0-only, disabled and absent from the current production compilation. Build/test dependencies and node_modules are not shipped as application code. The distribution license gate rejects unreviewed production licenses.
- MSI installs `LICENSE`, this notice, and `licenses.zip` alongside the client. That archive contains dependency notices, inventory and MPL source archives, including when the MSI is redistributed separately.
- Wisper and cloudflared were architecture references; their source is not incorporated into this application.

Release maintainers must retain these materials when redistributing artifacts. This file does not replace individual dependency licenses. See [license analysis](docs/licensing.md) and [release procedures](docs/releasing.md).
