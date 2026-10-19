// Inventory distributed Go dependencies and production Webpack modules, not npm tools.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const {execFileSync} = require('node:child_process');
const root = path.resolve(__dirname, '..');
const target = path.resolve(process.argv[2] || 'bin/licenses');
const web = path.resolve(process.argv[3] || 'web');
const frontendDist = path.resolve(process.argv[4] || path.join(web, 'dist'));
const hash = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
const runGo = (args, env = {}) => execFileSync('go', args, {
  cwd: root, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
  env: {...process.env, CGO_ENABLED: '0', ...env},
}).trim();
const notices = [];
const inventory = [];
const missing = [];
fs.mkdirSync(target, {recursive: true});

function noticeFiles(dir, prefix = '') {
  return fs.readdirSync(dir, {withFileTypes: true}).sort((a,b) => a.name.localeCompare(b.name)).flatMap(e => {
    if (e.isSymbolicLink() || ['node_modules', '.git'].includes(e.name)) return [];
    const relative = prefix + e.name;
    if (e.isDirectory()) return noticeFiles(path.join(dir, e.name), relative + '/');
    return /^(licen[sc]es?|copying|notice|copyright|patents)([.-]|$)/i.test(e.name) ? [relative] : [];
  });
}
function classify(text) {
  if (/Mozilla Public License\s*(?:Version|, v\.)?\s*2\.0/i.test(text)) return 'MPL-2.0';
  if (/Apache License[\s,]+(?:Version )?2\.0/i.test(text)) return 'Apache-2.0';
  if (/Permission is hereby granted/i.test(text)) return 'MIT';
  if (/Permission to use, copy, modify, and distribute/i.test(text)) return 'ISC';
  if (/Redistribution and use in source and binary forms/i.test(text)) {
    return /Neither the name|names of its contributors|3\.\s/i.test(text) ? 'BSD-3-Clause' : 'BSD-2-Clause';
  }
  if (/origin of this software must not be misrepresented/i.test(text)) return 'Zlib';
  return 'UNKNOWN';
}
function collect(item, directory) {
  const files = noticeFiles(directory);
  // Some packages (e.g. is-mobile) publish the complete grant in README only.
  if (!files.length && item.license === 'MIT') {
    const readme = fs.readdirSync(directory).find(f => /^readme(?:\.|$)/i.test(f));
    if (readme && /Permission is hereby granted[\s\S]*THE SOFTWARE IS PROVIDED/i.test(fs.readFileSync(path.join(directory,readme),'utf8'))) files.push(readme);
  }
  item.notices = files;
  if (!files.length) missing.push(`${item.name}@${item.version}: missing license text`);
  for (const file of files) notices.push(`\n${'='.repeat(72)}\n${item.name} ${item.version} — ${file}\nSource: ${item.source}\n\n${fs.readFileSync(path.join(directory,file), 'utf8')}\n`);
  inventory.push(item);
}
const modules = new Map();
for (const [GOOS, GOARCH] of [['windows','amd64'], ['linux','amd64'], ['linux','arm64']]) {
  const lines = runGo(['list','-mod=readonly','-deps','-f',
    '{{if .Module}}{{if .Module.Replace}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Replace.Dir}}{{else}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}{{end}}','./cmd/...'], {GOOS, GOARCH});
  for (const line of lines.split(/\r?\n/)) {
    const [name,version,directory] = line.split('|');
    if (version) modules.set(name, {name,version,directory});
  }
}
for (const {name,version,directory} of [...modules.values()].sort((a,b)=>a.name.localeCompare(b.name))) {
  const files = noticeFiles(directory);
  const primary = files.find(f => !f.includes('/') && /^(licen[sc]e|copying)([.-]|$)/i.test(f));
  const license = primary ? classify(fs.readFileSync(path.join(directory,primary),'utf8')) : 'UNKNOWN';
  const item = {ecosystem:'go',name,version,license,source:`https://${name}`,modified:name === 'github.com/go-gost/x'};
  if (!['MIT','ISC','BSD-2-Clause','BSD-3-Clause','Zlib','Apache-2.0','MPL-2.0'].includes(license)) missing.push(`${name}: unreviewed license ${license}`);
  if (license === 'MPL-2.0') {
    // Ship checksum-verified source rather than relying on a URL's availability.
    const downloaded = JSON.parse(runGo(['mod','download','-json',`${name}@${version}`]));
    if (downloaded.Error || !downloaded.Zip || !downloaded.Sum) throw Error(`Missing verified source for ${name}`);
    const relative = `sources/${name.replaceAll('/','_')}@${version}.zip`;
    fs.mkdirSync(path.join(target,'sources'), {recursive:true});
    fs.copyFileSync(downloaded.Zip, path.join(target,relative));
    item.sourceArchive = relative;
    item.sourceSHA256 = hash(path.join(target,relative));
    item.goChecksum = downloaded.Sum;
  }
  collect(item, directory);
}
const goroot = runGo(['env','GOROOT']);
for (const file of ['LICENSE','PATENTS']) if (fs.existsSync(path.join(goroot,file))) {
  notices.push(`\nGo toolchain ${runGo(['env','GOVERSION'])} — ${file}\n${fs.readFileSync(path.join(goroot,file),'utf8')}\n`);
}
const manifest = JSON.parse(fs.readFileSync(path.join(frontendDist,'license-modules.json'),'utf8'));
if (manifest.schemaVersion !== 1 || !manifest.packages?.length) throw Error('Missing production module evidence. Rebuild the frontend.');
const lock = JSON.parse(fs.readFileSync(path.join(root,'web/package-lock.json'),'utf8'));
for (const relative of manifest.packages) {
  if (!relative.startsWith('node_modules/') || relative.includes('..') || !lock.packages[relative]) throw Error(`Unpinned production package: ${relative}`);
  const entry = lock.packages[relative];
  const directory = path.join(web,relative);
  const pkg = JSON.parse(fs.readFileSync(path.join(directory,'package.json'),'utf8'));
  const license = entry.license || pkg.license;
  if (!['MIT','ISC','BSD-2-Clause','BSD-3-Clause','Apache-2.0','0BSD','Zlib','(MIT AND Zlib)','(MIT AND BSD-3-Clause)'].includes(license)) missing.push(`${pkg.name}: unreviewed production license ${license}`);
  if (pkg.version !== entry.version) throw Error(`Version mismatch: ${relative}`);
  const item = {ecosystem:'npm',name:pkg.name,version:entry.version,license,source:entry.resolved,integrity:entry.integrity,packagePath:relative};
  collect(item, directory);
}
// Webpack emits its own runtime boilerplate without a module.resource entry.
// Umi vendors that compiler, so its runtime notice must be collected explicitly.
const bundlerPath = 'node_modules/@umijs/bundler-webpack';
const bundler = JSON.parse(fs.readFileSync(path.join(web,bundlerPath,'package.json'),'utf8'));
if (bundler.version !== lock.packages[bundlerPath]?.version) throw Error('Unpinned Webpack bundler');
const webpackDir = path.join(web,bundlerPath,'compiled/webpack');
const webpack = JSON.parse(fs.readFileSync(path.join(webpackDir,'package.json'),'utf8'));
const webpackLicense = classify(fs.readFileSync(path.join(webpackDir,'LICENSE'),'utf8'));
if (webpackLicense !== 'MIT') missing.push('Review the generated Webpack runtime license');
collect({ecosystem:'npm',name:webpack.name,version:webpack.version,license:webpackLicense,
  source:`https://github.com/webpack/webpack/tree/v${webpack.version}`,role:'generated-runtime',
  bundledIn:`${bundler.name}@${bundler.version}`,integrity:lock.packages[bundlerPath].integrity},webpackDir);
if (missing.length) throw Error('Release license review failed:\n' + missing.join('\n'));
fs.writeFileSync(path.join(target,'THIRD_PARTY_LICENSES.txt'),
  'AltasCI Tunnel distributed dependency notices\nOriginal license terms govern each component. MPL source archives are in sources/.\n' + notices.join('\n'));
fs.writeFileSync(path.join(target,'dependency-inventory.json'), JSON.stringify({
  schemaVersion:2, scope:'Linked Go dependencies (Windows amd64, Linux amd64/arm64) and production Webpack modules; excludes npm build/test tools.',
  dependencies:inventory,
}, null, 2) + '\n');
console.log(`Release license review passed: ${inventory.length} distributed dependencies; MPL sources included.`);
