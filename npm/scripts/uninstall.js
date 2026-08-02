#!/usr/bin/env node
'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { agentsHome, wowdataHome } = require('../lib/paths');
const { ensureManagedChild } = require('../lib/fs');

function uninstall(options = {}) {
  const env = options.env || process.env;
  const home = wowdataHome(env);
  const skillsRoot = path.join(agentsHome(env), 'skills');
  const targetSkill = path.join(skillsRoot, 'wowdata');
  ensureManagedChild(skillsRoot, targetSkill);
  let skillRemoved = false;
  if (fs.existsSync(targetSkill)) {
    try {
      const marker = JSON.parse(fs.readFileSync(path.join(targetSkill, '.wowdata-managed.json'), 'utf8'));
      if (marker.package === '@follenfang/wowdata') {
        fs.rmSync(targetSkill, { recursive: true, force: true });
        skillRemoved = true;
      }
    } catch {}
  }

  const keepData = options.keepData ?? env.WOWDATA_KEEP_DATA === '1';
  if (keepData) {
    const bin = path.join(home, 'bin');
    ensureManagedChild(home, bin);
    fs.rmSync(bin, { recursive: true, force: true });
  } else if (fs.existsSync(home)) {
    const parent = path.dirname(home);
    ensureManagedChild(parent, home);
    fs.rmSync(home, { recursive: true, force: true });
  }
  return { home, keepData, skillRemoved };
}

if (require.main === module) {
  try {
    const result = uninstall();
    process.stderr.write(`wowdata uninstalled keepData=${result.keepData} skillRemoved=${result.skillRemoved}\n`);
  } catch (error) {
    process.stderr.write(`wowdata uninstall failed: ${error.message}\n`);
    process.exitCode = 1;
  }
}

module.exports = { uninstall };
