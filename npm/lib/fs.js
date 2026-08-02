const fs = require('node:fs');
const path = require('node:path');

function ensureManagedChild(root, target) {
  const relative = path.relative(path.resolve(root), path.resolve(target));
  if (!relative || relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    throw new Error(`path is outside managed root: ${target}`);
  }
}

function replacePath(source, target) {
  fs.rmSync(target, { recursive: true, force: true });
  fs.renameSync(source, target);
}

function copyDirectory(source, target) {
  fs.cpSync(source, target, { recursive: true, force: false, errorOnExist: true });
}

function timestamp() {
  return new Date().toISOString().replace(/[:.]/g, '-');
}

module.exports = { copyDirectory, ensureManagedChild, replacePath, timestamp };
