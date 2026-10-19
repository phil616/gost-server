// Rebuild the reviewable diff against the Go module checksum-verified baseline.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
function run(cmd, args) {
  const r = spawnSync(cmd, args, { cwd: root, encoding: 'utf8' });
  if (r.status > 1 || r.error) throw r.error || Error(r.stderr);
  return r.stdout;
}
const cache = run('go', ['env', 'GOMODCACHE']).trim();
const baseline = path.join(cache, 'github.com', 'go-gost', 'x@v0.16.0');
if (!fs.existsSync(baseline)) {
  run('go', ['mod', 'download', 'github.com/go-gost/x@v0.16.0']);
}
const files = ['handler/tunnel/hooks.go', 'handler/tunnel/handler.go', 'handler/tunnel/bind.go', 'handler/tunnel/connect.go', 'connector/tunnel/bind.go', 'service/service.go'];
let patch = '';
const manifest = { module: 'github.com/go-gost/x', version: 'v0.16.0', license: 'MIT', files: {} };
for (const file of files) {
  const old = path.join(baseline, file);
  const current = path.join(root, 'third_party/gost-x', file);
  let diff = run('git', ['-c', 'core.autocrlf=false', 'diff', '--no-index', '--', fs.existsSync(old) ? old : '/dev/null', current]);
  // Headers only; source context and code are preserved byte for byte.
  diff = diff.split('\n').map(line => {
    if (line.startsWith('diff --git ')) return `diff --git a/${file} b/${file}`;
    if (line.startsWith('--- ') && line !== '--- /dev/null') return `--- a/${file}`;
    if (line.startsWith('+++ ')) return `+++ b/${file}`;
    return line;
  }).join('\n');
  patch += diff;
  const sha = p => fs.existsSync(p) ? crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex') : null;
  manifest.files[file] = { upstream_sha256: sha(old), patched_sha256: sha(current) };
}
fs.mkdirSync(path.join(root, 'patches'), { recursive: true });
fs.writeFileSync(path.join(root, 'patches/gost-x-v0.16.0.patch'), patch);
fs.writeFileSync(path.join(root, 'patches/manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
