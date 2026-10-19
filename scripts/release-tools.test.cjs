const {test} = require('node:test');
const assert = require('node:assert/strict');
const {parseTag} = require('./release-metadata.cjs');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const {spawnSync} = require('node:child_process');
test('release tags map to MSI versions without silently truncating invalid tags', () => {
  assert.deepEqual(parseTag('v1.2.3'), {version:'1.2.3',msiVersion:'1.2.3',prerelease:false});
  assert.deepEqual(parseTag('v1.2.3-rc.1'), {version:'1.2.3-rc.1',msiVersion:'1.2.3',prerelease:true});
  assert.equal(parseTag('v255.255.65535').msiVersion,'255.255.65535');
  for (const tag of ['v01.2.3','v1.2','1.2.3','v1.2.3-01','v256.0.0','v1.256.0','v1.2.65536','v1.2.3+build','v1.2.3\nmalicious','v1.2.3-']) assert.throws(()=>parseTag(tag),tag);
});
test('license gate rejects AGPL, missing notices, unavailable and tampered MPL sources', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(),'altasci-license-test-'));
  try {
    fs.mkdirSync(path.join(directory,'sources'));
    fs.writeFileSync(path.join(directory,'THIRD_PARTY_LICENSES.txt'),'Test fixture notices');
    const source = path.join(directory,'sources/component.zip');
    fs.writeFileSync(source,'source fixture bytes');
    const deps = [
      {ecosystem:'go',name:'example/go',license:'MPL-2.0',notices:['LICENSE'],sourceArchive:'sources/component.zip',sourceSHA256:crypto.createHash('sha256').update(fs.readFileSync(source)).digest('hex'),goChecksum:'h1:fixture'},
      {ecosystem:'npm',name:'example/web',license:'MIT',notices:['LICENSE']},
    ];
    function check(dependencies) {
      fs.writeFileSync(path.join(directory,'dependency-inventory.json'),JSON.stringify({schemaVersion:2,dependencies}));
      return spawnSync(process.execPath,[path.join(__dirname,'verify-licenses.cjs'),directory],{encoding:'utf8'});
    }
    assert.equal(check(deps).status,0);
    assert.match(check([deps[0],{...deps[1],license:'AGPL-3.0-only'}]).stderr,/Unreviewed dependency/);
    assert.match(check([deps[0],{...deps[1],notices:[]}]).stderr,/Unreviewed dependency/);
    assert.match(check([{...deps[0],sourceArchive:'../escape.zip'},deps[1]]).stderr,/Missing source/);
    fs.writeFileSync(source,'tampered bytes');
    assert.match(check(deps).stderr,/Invalid source/);
    fs.unlinkSync(source);
    assert.notEqual(check(deps).status,0);
  } finally {fs.rmSync(directory,{recursive:true,force:true});}
});
