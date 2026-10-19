// Maintained documentation checks; Node standard library only.
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '..');
const errors = [];
function walk(dir) {
  return fs.readdirSync(dir, {withFileTypes: true}).flatMap(e => {
    const file = path.join(dir, e.name);
    return e.isDirectory() ? walk(file) : e.isFile() && file.endsWith('.md') ? [file] : [];
  });
}
function prose(text) {
  let fenced = false;
  return text.split(/\r?\n/).filter(line => {
    if (/^\s*```/.test(line)) {fenced = !fenced; return false;}
    return !fenced;
  }).join('\n');
}
function anchors(file) {
  const counts = new Map();
  return new Set([...prose(fs.readFileSync(file, 'utf8')).matchAll(/^#{1,6}\s+(.+)$/gm)].map(m => {
    const base = m[1].toLowerCase().replace(/<[^>]*>/g, '').replace(/[^\p{L}\p{N}_ -]/gu, '').replace(/ /g, '-');
    const n = counts.get(base) || 0; counts.set(base, n + 1);
    return n ? `${base}-${n}` : base;
  }));
}
const files = ['README.md', 'CONTRIBUTING.md', 'SECURITY.md', 'THIRD_PARTY_NOTICES.md', 'patches/README.md'].map(f => path.join(root,f)).concat(walk(path.join(root,'docs')));
let links = 0;
for (const file of files) {
  for (const m of prose(fs.readFileSync(file,'utf8')).matchAll(/!?\[[^\]\n]*\]\(([^)\n]+)\)/g)) {
    let target = m[1].trim();
    target = target.startsWith('<') ? target.slice(1,target.indexOf('>')) : target.replace(/\s+["'].*$/, '');
    if (/^[a-z][a-z0-9+.-]*:/i.test(target) || target.startsWith('//')) continue;
    links++;
    const [pathname, hash] = target.split('#',2);
    let resolved;
    try {resolved = pathname ? path.resolve(path.dirname(file),decodeURIComponent(pathname)) : file;}
    catch {errors.push(`${file}: invalid ${target}`); continue;}
    if (!fs.existsSync(resolved)) errors.push(`${path.relative(root,file)}: missing ${target}`);
    else if (hash && resolved.endsWith('.md') && !anchors(resolved).has(decodeURIComponent(hash))) errors.push(`${path.relative(root,file)}: missing anchor ${target}`);
  }
}
const yaml = fs.readFileSync(path.join(root,'docs/openapi.yaml'),'utf8');
const declared = new Set(), stack = [];
for (const line of yaml.split(/\r?\n/)) {
  const m = /^(\s*)(?:'([^']+)'|"([^"]+)"|([A-Za-z0-9_./{}-]+)):\s*/.exec(line);
  if (!m) continue;
  const indent=m[1].length;
  while (stack.length && stack.at(-1).indent >= indent) stack.pop();
  stack.push({indent,key:m[2]||m[3]||m[4]});
  declared.add('#/'+stack.map(x=>x.key.replace(/~/g,'~0').replace(/\//g,'~1')).join('/'));
}
for (const m of yaml.matchAll(/\$ref:\s*['"]?(#[^'"\s,}]+)/g)) if (!declared.has(m[1])) errors.push(`OpenAPI: undefined ${m[1]}`);
const paths=new Map(); let current;
for (const line of yaml.split(/\r?\n/)) {
  const p=/^  (\/[^:]+):\s*$/.exec(line);
  if(p){current=p[1];paths.set(current,new Set());continue;}
  const m=/^    (get|post|patch|delete|put):/.exec(line);
  if(m&&current)paths.get(current).add(m[1]);
}
let routeCount=0;
const routes=fs.readFileSync(path.join(root,'internal/control/http.go'),'utf8');
for(const m of routes.matchAll(/\.(Get|Post|Patch|Delete|Put)\("(\/api\/v1[^"\n]*)"/g)){
  routeCount++;const endpoint=m[2].slice('/api/v1'.length);
  if(!paths.get(endpoint)?.has(m[1].toLowerCase()))errors.push(`OpenAPI: missing ${m[1]} ${endpoint}`);
}
for(const old of ['AltasCITunnel.md','docs/plan.md','docs/research','docs/技术实现.md','docs/可行性调研与实施方案.md','docs/运维与恢复.md','docs/系统验收.md','docs/验证报告.md','docs/自有服务器临时测试.md']) if(fs.existsSync(path.join(root,old)))errors.push(`Retired entry still exists: ${old}`);
if(errors.length){errors.forEach(e=>process.stderr.write(e+'\n'));process.exitCode=1;}
else console.log(`Documentation OK: ${files.length} Markdown files, ${links} local links, ${routeCount} REST routes and OpenAPI references.`);
