const fs = require('node:fs');
function parseTag(tag) {
  const m = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(tag);
  if (!m || m[0] !== tag || m[4]?.split('.').some(s => /^\d+$/.test(s) && s.length>1 && s[0]==='0')) throw Error('Expected vMAJOR.MINOR.PATCH[-prerelease]');
  if (+m[1]>255 || +m[2]>255 || +m[3]>65535) throw Error('Version exceeds Windows MSI limits (255.255.65535)');
  return {version:tag.slice(1),msiVersion:m.slice(1,4).join('.'),prerelease:!!m[4]};
}
if (require.main === module) {
  const isTag = process.env.GITHUB_REF_TYPE === 'tag';
  const result = isTag ? parseTag(process.env.GITHUB_REF_NAME) : {
    version:`0.0.0-ci.${process.env.GITHUB_RUN_NUMBER || '0'}.${process.env.GITHUB_RUN_ATTEMPT || '1'}`,
    msiVersion:'0.0.0',prerelease:true,
  };
  const output = Object.entries(result).map(([k,v])=>`${k}=${v}`).join('\n')+'\n';
  if (process.env.GITHUB_OUTPUT) fs.appendFileSync(process.env.GITHUB_OUTPUT,output);
  process.stdout.write(output);
}
module.exports = {parseTag};
