'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { npmInvocation, validUpdateVersion } = require('../bin/wowdata');

test('uses cmd.exe to launch npm.cmd on Windows', () => {
  const invocation = npmInvocation(['uninstall', '-g', '@follenfang/wowdata'], 'win32', { ComSpec: 'C:\\Windows\\System32\\cmd.exe' });
  assert.equal(invocation.command, 'C:\\Windows\\System32\\cmd.exe');
  assert.deepEqual(invocation.args, ['/d', '/s', '/c', 'npm.cmd', 'uninstall', '-g', '@follenfang/wowdata']);
});

test('accepts only latest or exact semantic versions', () => {
  assert.equal(validUpdateVersion('latest'), true);
  assert.equal(validUpdateVersion('1.2.3'), true);
  assert.equal(validUpdateVersion('1.2.3-beta.1'), true);
  assert.equal(validUpdateVersion('latest & whoami'), false);
  assert.equal(validUpdateVersion('^1.2.3'), false);
});
