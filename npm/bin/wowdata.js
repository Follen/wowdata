#!/usr/bin/env node
'use strict';

const { spawnSync } = require('node:child_process');
const path = require('node:path');
const { binaryName, wowdataHome } = require('../lib/paths');
const { uninstall } = require('../scripts/uninstall');

function npmInvocation(args, platform = process.platform, env = process.env) {
  if (platform === 'win32') {
    return {
      command: env.ComSpec || 'cmd.exe',
      args: ['/d', '/s', '/c', 'npm.cmd', ...args]
    };
  }
  return { command: 'npm', args };
}

function validUpdateVersion(version) {
  return version === 'latest' || /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version);
}

function runLifecycle(command, args) {
  const env = { ...process.env };
  let npmArgs;
  if (command === 'update') {
    const index = args.indexOf('--version');
    const version = index >= 0 && args[index + 1] ? args[index + 1] : 'latest';
    if (!validUpdateVersion(version)) {
      process.stdout.write(`${JSON.stringify({ ok: false, command, data: { package: '@follenfang/wowdata' }, warnings: [], error: { code: 'invalid_version', message: 'version must be latest or an exact semantic version' } }, null, 2)}\n`);
      process.exit(1);
    }
    npmArgs = ['install', '-g', `@follenfang/wowdata@${version}`];
    process.stderr.write(`update package=@follenfang/wowdata version=${version}\n`);
  } else {
    const keepData = args.includes('--keep-data');
    if (keepData) env.WOWDATA_KEEP_DATA = '1';
    uninstall({ env, keepData });
    npmArgs = ['uninstall', '-g', '@follenfang/wowdata'];
    process.stderr.write(`uninstall package=@follenfang/wowdata keepData=${keepData}\n`);
  }
  const invocation = npmInvocation(npmArgs, process.platform, env);
  const result = spawnSync(invocation.command, invocation.args, { env, encoding: 'utf8' });
  if (result.stderr) process.stderr.write(result.stderr);
  const ok = result.status === 0;
  process.stdout.write(`${JSON.stringify({ ok, command, data: { package: '@follenfang/wowdata', keepData: command === 'uninstall' ? args.includes('--keep-data') : undefined, output: (result.stdout || '').trim() }, warnings: [], ...(ok ? {} : { error: { code: `npm_${command}_failed`, message: result.error?.message || `npm exited ${result.status}` } }) }, null, 2)}\n`);
  process.exit(result.status ?? 1);
}

function main(args) {
  if (args[0] === 'update' || args[0] === 'uninstall') runLifecycle(args[0], args.slice(1));

  const binary = path.join(wowdataHome(), 'bin', binaryName());
  const result = spawnSync(binary, args, { stdio: 'inherit' });
  if (result.error) {
    process.stderr.write(`wowdata launch failed: ${result.error.message}; run npm install -g @follenfang/wowdata\n`);
    process.exit(1);
  }
  process.exit(result.status ?? 1);
}

if (require.main === module) main(process.argv.slice(2));

module.exports = { npmInvocation, validUpdateVersion };
