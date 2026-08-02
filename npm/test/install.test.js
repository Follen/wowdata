'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const assert = require('node:assert/strict');
const { install } = require('../scripts/install');
const { uninstall } = require('../scripts/uninstall');

test('installs binary and Skill, backs up unknown Skill, and uninstalls data', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'wowdata-npm-'));
  const home = path.join(root, '.wowdata');
  const agents = path.join(root, '.agents');
  const oldSkill = path.join(agents, 'skills', 'wowdata');
  fs.mkdirSync(oldSkill, { recursive: true });
  fs.writeFileSync(path.join(oldSkill, 'user.txt'), 'keep');
  const sourceBinary = path.join(root, process.platform === 'win32' ? 'source.exe' : 'source');
  fs.writeFileSync(sourceBinary, 'binary');

  const result = install({ env: { WOWDATA_HOME: home, AGENTS_HOME: agents }, sourceBinary, platform: process.platform, arch: process.arch });
  assert.equal(fs.existsSync(result.targetBinary), true);
  assert.equal(fs.existsSync(path.join(result.targetSkill, 'SKILL.md')), true);
  const backups = fs.readdirSync(path.join(agents, 'skills')).filter((name) => name.startsWith('wowdata.backup-'));
  assert.equal(backups.length, 1);
  assert.equal(fs.readFileSync(path.join(agents, 'skills', backups[0], 'user.txt'), 'utf8'), 'keep');

  uninstall({ env: { WOWDATA_HOME: home, AGENTS_HOME: agents }, keepData: false });
  assert.equal(fs.existsSync(home), false);
  assert.equal(fs.existsSync(result.targetSkill), false);
  assert.equal(fs.existsSync(path.join(agents, 'skills', backups[0], 'user.txt')), true);
});
