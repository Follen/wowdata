#!/usr/bin/env node
'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { agentsHome, assetName, binaryName, wowdataHome } = require('../lib/paths');
const { copyDirectory, ensureManagedChild, replacePath, timestamp } = require('../lib/fs');

const packageRoot = path.resolve(__dirname, '..', '..');
const packageJSON = require(path.join(packageRoot, 'package.json'));

function install(options = {}) {
  const env = options.env || process.env;
  const home = wowdataHome(env);
  const agents = agentsHome(env);
  const dirs = ['bin', 'config', 'profiles', 'builds', 'cache/casc', 'cache/dbd', 'cache/listfile', 'cache/tact', 'cache/manifests', 'state', 'tmp', 'locks'];
  for (const dir of dirs) fs.mkdirSync(path.join(home, dir), { recursive: true });

  const sourceBinary = options.sourceBinary || path.join(packageRoot, 'dist', assetName(options.platform, options.arch));
  if (!fs.existsSync(sourceBinary)) throw new Error(`binary asset is missing: ${sourceBinary}`);
  const targetBinary = path.join(home, 'bin', binaryName(options.platform));
  const binaryTemp = path.join(home, 'tmp', `${binaryName(options.platform)}.${process.pid}.tmp`);
  fs.copyFileSync(sourceBinary, binaryTemp);
  fs.chmodSync(binaryTemp, 0o755);
  replacePath(binaryTemp, targetBinary);

  const sourceSkill = path.join(packageRoot, 'skill', 'wowdata');
  const skillsRoot = path.join(agents, 'skills');
  const targetSkill = path.join(skillsRoot, 'wowdata');
  fs.mkdirSync(skillsRoot, { recursive: true });
  ensureManagedChild(skillsRoot, targetSkill);
  if (fs.existsSync(targetSkill)) {
    const markerPath = path.join(targetSkill, '.wowdata-managed.json');
    let managed = false;
    try {
      const marker = JSON.parse(fs.readFileSync(markerPath, 'utf8'));
      managed = marker.package === '@follenfang/wowdata';
    } catch {}
    if (!managed) {
      let backup = path.join(skillsRoot, `wowdata.backup-${timestamp()}`);
      let suffix = 1;
      while (fs.existsSync(backup)) backup = path.join(skillsRoot, `wowdata.backup-${timestamp()}-${suffix++}`);
      fs.renameSync(targetSkill, backup);
    }
  }
  const skillTemp = path.join(skillsRoot, `.wowdata.${process.pid}.tmp`);
  fs.rmSync(skillTemp, { recursive: true, force: true });
  copyDirectory(sourceSkill, skillTemp);
  const marker = {
    schema: 'wowdata.skill-install.v1',
    package: '@follenfang/wowdata',
    version: packageJSON.version,
    installedAt: new Date().toISOString()
  };
  fs.writeFileSync(path.join(skillTemp, '.wowdata-managed.json'), `${JSON.stringify(marker, null, 2)}\n`);
  replacePath(skillTemp, targetSkill);

  const sha256 = crypto.createHash('sha256').update(fs.readFileSync(targetBinary)).digest('hex');
  const state = { schema: 'wowdata.install.v1', package: '@follenfang/wowdata', version: packageJSON.version, binary: targetBinary, sha256 };
  fs.writeFileSync(path.join(home, 'state', 'install.json'), `${JSON.stringify(state, null, 2)}\n`);
  return { home, targetBinary, targetSkill, sha256 };
}

if (require.main === module) {
  try {
    const result = install();
    process.stderr.write(`wowdata installed binary=${result.targetBinary} skill=${result.targetSkill}\n`);
  } catch (error) {
    process.stderr.write(`wowdata install failed: ${error.message}\n`);
    process.exitCode = 1;
  }
}

module.exports = { install };
