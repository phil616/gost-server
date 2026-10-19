const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const directory = path.resolve(process.argv[2] || 'web/dist/licenses');
const manifest = JSON.parse(fs.readFileSync(path.join(directory,'dependency-inventory.json'),'utf8'));
const allowed = new Set(['MIT','ISC','BSD-2-Clause','BSD-3-Clause','Apache-2.0','0BSD','Zlib','MPL-2.0','(MIT AND Zlib)','(MIT AND BSD-3-Clause)']);
if (manifest.schemaVersion !== 2 || !manifest.dependencies?.length) throw Error('Incomplete release inventory');
if (!fs.statSync(path.join(directory,'THIRD_PARTY_LICENSES.txt')).size) throw Error('Empty dependency notices');
for (const dep of manifest.dependencies) {
  if (!allowed.has(dep.license) || !dep.notices?.length) throw Error(`Unreviewed dependency: ${dep.name}`);
  if (dep.license === 'MPL-2.0') {
    if (!dep.sourceArchive?.startsWith('sources/') || dep.sourceArchive.includes('..')) throw Error(`Missing source: ${dep.name}`);
    const digest = crypto.createHash('sha256').update(fs.readFileSync(path.join(directory,dep.sourceArchive))).digest('hex');
    if (digest !== dep.sourceSHA256 || !dep.goChecksum?.startsWith('h1:')) throw Error(`Invalid source: ${dep.name}`);
  }
}
for (const ecosystem of ['go','npm']) if (!manifest.dependencies.some(d=>d.ecosystem===ecosystem)) throw Error(`Missing ${ecosystem} inventory`);
console.log(`License payload OK: ${manifest.dependencies.length} dependencies and all required sources.`);
