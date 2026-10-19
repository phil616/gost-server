// Record packages contributing JavaScript/CSS to the production compilation.
// This is deliberately separate from the inventory of installed build tools.
const fs = require('node:fs');
const path = require('node:path');

module.exports = class LicenseManifestPlugin {
  apply(compiler) {
    compiler.hooks.done.tap('LicenseManifestPlugin', (stats) => {
      if (stats.hasErrors()) return;
      const root = compiler.context;
      const locked = JSON.parse(fs.readFileSync(path.join(root, 'package-lock.json'), 'utf8')).packages;
      const packages = new Set();
      const visited = new Set();
      function visit(module) {
        if (visited.has(module)) return;
        visited.add(module);
        if (module.resource) {
          let dir = path.dirname(module.resource.split('?')[0]);
          while (dir.startsWith(root + path.sep)) {
            const relative = path.relative(root, dir).replaceAll('\\', '/');
            if (locked[relative]) {
              packages.add(relative);
              break;
            }
            dir = path.dirname(dir);
          }
        }
        if (module.modules) for (const child of module.modules) visit(child);
      }
      for (const module of stats.compilation.modules) visit(module);
      fs.writeFileSync(path.join(compiler.outputPath, 'license-modules.json'),
        JSON.stringify({schemaVersion: 1, version: process.env.ALTASCI_VERSION || 'dev', packages: [...packages].sort()}, null, 2) + '\n');
    });
  }
};
